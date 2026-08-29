package configurationhub

import (
	"errors"
	"runtime"
	"strings"
)

func parseCommandLine(value string) ([]string, error) {
	if runtime.GOOS == "windows" {
		return parseWindowsCommandLine(value)
	}
	return parsePosixCommandLine(value)
}

func parsePosixCommandLine(value string) ([]string, error) {
	parser := posixCommandLineParser{chars: []rune(value)}
	return parser.parse()
}

type commandLineCharacter rune

func (char commandLineCharacter) isSpace() bool {
	return char == ' ' || char == '\t' || char == '\n'
}

type posixCommandLineParser struct {
	arguments []string
	current   strings.Builder
	quote     commandLineCharacter
	escaped   bool
	chars     []rune
	index     int
}

func (parser *posixCommandLineParser) parse() ([]string, error) {
	for parser.hasNext() {
		parser.consume()
	}
	if parser.escaped || parser.quote != 0 {
		return nil, errors.New("unterminated escape or quote")
	}
	parser.flush()
	return parser.arguments, nil
}

func (parser *posixCommandLineParser) consume() {
	char := parser.next()
	if parser.escaped {
		parser.current.WriteRune(rune(char))
		parser.escaped = false
		return
	}
	if char == '\\' && parser.quote != '\'' {
		parser.escaped = true
		return
	}
	if parser.quote != 0 {
		if char == parser.quote {
			parser.quote = 0
			return
		}
		parser.current.WriteRune(rune(char))
		return
	}
	if char == '\'' || char == '"' {
		parser.quote = char
		return
	}
	if char.isSpace() {
		parser.flush()
		return
	}
	parser.current.WriteRune(rune(char))
}

func (parser *posixCommandLineParser) next() commandLineCharacter {
	char := commandLineCharacter(parser.chars[parser.index])
	parser.index++
	return char
}

func (parser *posixCommandLineParser) hasNext() bool {
	return parser.index < len(parser.chars)
}

func (parser *posixCommandLineParser) flush() {
	if parser.current.Len() == 0 {
		return
	}
	parser.arguments = append(parser.arguments, parser.current.String())
	parser.current.Reset()
}

type backslashRun int

type windowsCommandLineParser struct {
	arguments []string
	current   strings.Builder
	chars     []rune
	index     int
	quoted    bool
	started   bool
}

func parseWindowsCommandLine(value string) ([]string, error) {
	parser := windowsCommandLineParser{chars: []rune(value)}
	return parser.parse()
}

func (parser *windowsCommandLineParser) parse() ([]string, error) {
	for parser.hasNext() {
		parser.consume()
	}
	if parser.quoted {
		return nil, errors.New("unterminated quote")
	}
	parser.flush()
	return parser.arguments, nil
}

func (parser *windowsCommandLineParser) consume() {
	char := parser.next()
	if char == '\\' {
		parser.consumeBackslashes()
		return
	}
	if char == '"' {
		parser.consumeQuote()
		return
	}
	if char.isSpace() {
		if parser.quoted {
			parser.write(char)
			return
		}
		parser.flush()
		return
	}
	parser.write(char)
}

func (parser *windowsCommandLineParser) consumeQuote() {
	parser.started = true
	if parser.quoted {
		if parser.atQuote() {
			parser.current.WriteRune('"')
			parser.index++
			return
		}
	}
	parser.quoted = !parser.quoted
}

func (parser *windowsCommandLineParser) consumeBackslashes() {
	count := parser.consumeBackslashRun()
	if !parser.atQuote() {
		parser.writeBackslashes(count)
		return
	}
	parser.writeBackslashes(count / 2)
	parser.started = true
	if count%2 == 1 {
		parser.current.WriteRune('"')
		parser.index++
		return
	}
	parser.consumeEvenBackslashesBeforeQuote()
}

func (parser *windowsCommandLineParser) consumeBackslashRun() backslashRun {
	count := backslashRun(1)
	for parser.atBackslash() {
		parser.index++
		count++
	}
	return count
}

func (parser *windowsCommandLineParser) consumeEvenBackslashesBeforeQuote() {
	if parser.hasAdjacentQuotes() {
		parser.current.WriteRune('"')
		parser.index += 2
		return
	}
	parser.quoted = !parser.quoted
	parser.index++
}

func (parser *windowsCommandLineParser) hasAdjacentQuotes() bool {
	if !parser.quoted {
		return false
	}
	if !parser.atQuote() {
		return false
	}
	if parser.index+1 >= len(parser.chars) {
		return false
	}
	return parser.chars[parser.index+1] == '"'
}

func (parser *windowsCommandLineParser) atBackslash() bool {
	return parser.hasNext() && parser.chars[parser.index] == '\\'
}

func (parser *windowsCommandLineParser) atQuote() bool {
	return parser.hasNext() && parser.chars[parser.index] == '"'
}

func (parser *windowsCommandLineParser) write(char commandLineCharacter) {
	parser.current.WriteRune(rune(char))
	parser.started = true
}

func (parser *windowsCommandLineParser) writeBackslashes(count backslashRun) {
	for range count {
		parser.current.WriteRune('\\')
	}
	if count > 0 {
		parser.started = true
	}
}

func (parser *windowsCommandLineParser) next() commandLineCharacter {
	char := commandLineCharacter(parser.chars[parser.index])
	parser.index++
	return char
}

func (parser *windowsCommandLineParser) hasNext() bool {
	return parser.index < len(parser.chars)
}

func (parser *windowsCommandLineParser) flush() {
	if !parser.started {
		return
	}
	parser.arguments = append(parser.arguments, parser.current.String())
	parser.current.Reset()
	parser.started = false
}
