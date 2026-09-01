package repolock

import "errors"

var ErrHeld = errors.New("another glm-worker is already running for this repository")
