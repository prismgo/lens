package lens

import (
	"errors"
	"os"
)

// mergeCloseError closes a resource and joins the close error into the named
// return error so cleanup failures are not silently dropped.
func mergeCloseError(errp *error, close func() error) {
	if closeErr := close(); closeErr != nil {
		*errp = errors.Join(*errp, closeErr)
	}
}

// mergeRemoveAllError removes a temporary tree and joins any cleanup failure
// into the named return error.
func mergeRemoveAllError(errp *error, path string) {
	if removeErr := os.RemoveAll(path); removeErr != nil {
		*errp = errors.Join(*errp, removeErr)
	}
}
