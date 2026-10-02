package main

// splitShellCommand reads a shell command line the way a Caller Agent hands
// it to its shell tool, closely enough to find git commands in it. It splits
// on the control operators, removes quoting, drops redirections, reads
// here-document bodies, and keeps command substitutions as opaque words. It
// expands nothing: a word built from a variable stays as written.

import (
	"errors"
	"slices"
	"strings"
)

// shellWords is one simple command's words, quoting removed.
type shellWords []string

// shellSegment is one simple command in a command line.
type shellSegment struct {
	words shellWords
	// operator is the control operator that ends the command: &&, ||, |, |&,
	// &, ;, a newline, ( or ), or empty at the end of the input.
	operator string
	// depth counts the ( ) subshells the command runs in.
	depth int
}

var errUnterminatedQuote = errors.New("unterminated quote in the command")

// commandSubstitution stands in for the value of $(...) or `...`, which the
// hook cannot know before the shell runs it.
const commandSubstitution = "$(...)"

type shellLexer struct {
	input    string
	position int
	segments []shellSegment
	words    shellWords
	word     strings.Builder
	inWord   bool
	// nextWord says what the next finished word is: an argument, a
	// redirection target to drop, or a here-document delimiter.
	nextWord     shellWordRole
	heredocs     []string
	nestingDepth int
}

type shellWordRole int

const (
	wordArgument shellWordRole = iota
	wordRedirectTarget
	wordHeredocDelimiter
)

func splitShellCommand(command string) ([]shellSegment, error) {
	lexer := &shellLexer{input: command}
	if err := lexer.scan(endOfInput); err != nil {
		return nil, err
	}
	return lexer.segments, nil
}

// endOfInput is the closer of a top-level scan, which only the end of the
// input ends.
const endOfInput byte = 0

// scan reads until the unnested closer, the ")" that ends a $(...)
// substitution, or until the input ends, which is an error for a closer.
func (lexer *shellLexer) scan(closer byte) error {
	for lexer.position < len(lexer.input) {
		character := lexer.input[lexer.position]
		if character == closer && lexer.nestingDepth == 0 {
			lexer.position++
			return nil
		}
		if err := lexer.step(character); err != nil {
			return err
		}
	}
	lexer.endSegment("")
	if closer != endOfInput {
		return errUnterminatedQuote
	}
	return nil
}

func (lexer *shellLexer) step(character byte) error {
	switch character {
	case ' ', '\t':
		lexer.endWord()
		lexer.position++
	case '\n':
		lexer.endSegment("\n")
		lexer.position++
		lexer.readHeredocBodies()
	case ';', '(', ')':
		lexer.endSegment(string(character))
		lexer.nest(character)
		lexer.position++
	case '&', '|':
		lexer.controlOperator(character)
	case '<', '>':
		lexer.redirection()
	case '#':
		lexer.comment()
	default:
		return lexer.wordCharacter(character)
	}
	return nil
}

func (lexer *shellLexer) nest(character byte) {
	switch character {
	case '(':
		lexer.nestingDepth++
	case ')':
		lexer.nestingDepth--
	}
}

var twoCharacterControlOperators = []string{"&&", "||", "|&"}

// controlOperator ends the segment at &&, ||, |&, |, and a background &, and
// reads &> and &>> as redirections.
func (lexer *shellLexer) controlOperator(character byte) {
	if character == '&' && lexer.peek(1) == '>' {
		lexer.redirection()
		return
	}
	operator := string(character)
	if pair := string([]byte{character, lexer.peek(1)}); slices.Contains(twoCharacterControlOperators, pair) {
		operator = pair
	}
	lexer.endSegment(operator)
	lexer.position += len(operator)
}

// redirection drops an operator such as 2>&1, >>file, or <<'EOF', and the
// word it names. A leading file descriptor number belongs to the operator.
func (lexer *shellLexer) redirection() {
	if lexer.inWord && strings.Trim(lexer.word.String(), "0123456789") == "" {
		lexer.word.Reset()
		lexer.inWord = false
	}
	lexer.endWord()
	start := lexer.position
	for lexer.position < len(lexer.input) && strings.IndexByte("<>&|-", lexer.input[lexer.position]) >= 0 {
		lexer.position++
	}
	operator := lexer.input[start:lexer.position]
	switch {
	case strings.HasPrefix(operator, "<<<"):
		lexer.nextWord = wordRedirectTarget
	case strings.HasPrefix(operator, "<<"):
		lexer.nextWord = wordHeredocDelimiter
	case strings.HasSuffix(operator, "&-"):
	default:
		lexer.nextWord = wordRedirectTarget
	}
}

func (lexer *shellLexer) comment() {
	if lexer.inWord {
		lexer.word.WriteByte('#')
		lexer.position++
		return
	}
	for lexer.position < len(lexer.input) && lexer.input[lexer.position] != '\n' {
		lexer.position++
	}
}

func (lexer *shellLexer) wordCharacter(character byte) error {
	lexer.inWord = true
	switch {
	case character == '\\':
		lexer.position++
		if next := lexer.peek(0); next != '\n' && next != 0 {
			lexer.word.WriteByte(next)
		}
		lexer.position++
	case character == '\'':
		return lexer.singleQuoted()
	case character == '"':
		return lexer.doubleQuoted()
	case lexer.startsSubstitution(character):
		return lexer.substitution()
	default:
		lexer.word.WriteByte(character)
		lexer.position++
	}
	return nil
}

func (lexer *shellLexer) singleQuoted() error {
	end := strings.IndexByte(lexer.input[lexer.position+1:], '\'')
	if end < 0 {
		return errUnterminatedQuote
	}
	lexer.word.WriteString(lexer.input[lexer.position+1 : lexer.position+1+end])
	lexer.position += end + 2
	return nil
}

func (lexer *shellLexer) doubleQuoted() error {
	lexer.position++
	for lexer.position < len(lexer.input) {
		character := lexer.input[lexer.position]
		switch {
		case character == '"':
			lexer.position++
			return nil
		case character == '\\' && strings.IndexByte("$`\"\\\n", lexer.peek(1)) >= 0:
			if next := lexer.peek(1); next != '\n' {
				lexer.word.WriteByte(next)
			}
			lexer.position += 2
		case lexer.startsSubstitution(character):
			if err := lexer.substitution(); err != nil {
				return err
			}
		default:
			lexer.word.WriteByte(character)
			lexer.position++
		}
	}
	return errUnterminatedQuote
}

func (lexer *shellLexer) startsSubstitution(character byte) bool {
	if character == '$' {
		return lexer.peek(1) == '('
	}
	return character == '`'
}

// substitution skips $(...) or `...` as one opaque value. $(...) is scanned
// as a nested command line so its quotes and here-documents cannot end it
// early.
func (lexer *shellLexer) substitution() error {
	lexer.word.WriteString(commandSubstitution)
	if lexer.input[lexer.position] == '`' {
		end := strings.IndexByte(lexer.input[lexer.position+1:], '`')
		if end < 0 {
			return errUnterminatedQuote
		}
		lexer.position += end + 2
		return nil
	}
	nested := &shellLexer{input: lexer.input, position: lexer.position + 2}
	if err := nested.scan(')'); err != nil {
		return err
	}
	lexer.position = nested.position
	return nil
}

// readHeredocBodies skips the bodies of the here-documents the finished line
// opened, each up to its delimiter line.
func (lexer *shellLexer) readHeredocBodies() {
	for _, delimiter := range lexer.heredocs {
		for lexer.position < len(lexer.input) {
			line, rest, _ := strings.Cut(lexer.input[lexer.position:], "\n")
			lexer.position = len(lexer.input) - len(rest)
			if strings.TrimLeft(line, "\t") == delimiter {
				break
			}
		}
	}
	lexer.heredocs = nil
}

func (lexer *shellLexer) peek(offset int) byte {
	if lexer.position+offset < len(lexer.input) {
		return lexer.input[lexer.position+offset]
	}
	return 0
}

func (lexer *shellLexer) endWord() {
	if !lexer.inWord {
		return
	}
	word := lexer.word.String()
	lexer.word.Reset()
	lexer.inWord = false
	switch lexer.nextWord {
	case wordArgument:
		lexer.words = append(lexer.words, word)
	case wordHeredocDelimiter:
		lexer.heredocs = append(lexer.heredocs, word)
	case wordRedirectTarget:
	}
	lexer.nextWord = wordArgument
}

// endSegment ends the command at operator. A subshell's commands end before
// its ( or ) changes the depth, so they keep the depth they ran at.
func (lexer *shellLexer) endSegment(operator string) {
	lexer.endWord()
	if len(lexer.words) > 0 {
		lexer.segments = append(lexer.segments, shellSegment{words: lexer.words, operator: operator, depth: lexer.nestingDepth})
	}
	lexer.words = nil
}
