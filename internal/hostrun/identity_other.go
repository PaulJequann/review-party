//go:build unix && !linux && !darwin

package hostrun

import "errors"

var errIdentityUnavailable = errors.New("process identity is unavailable on this platform")

func bootID() (string, error) { return "", errIdentityUnavailable }

func identify(int) (Identity, error) { return Identity{}, errIdentityUnavailable }
