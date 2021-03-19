package errors

import "github.com/giantswarm/microerror"

var NotFound = &microerror.Error{
	Kind: "NotFoundError",
}

// IsNotFound asserts NotFound.
func IsNotFound(err error) bool {
	return microerror.Cause(err) == NotFound
}
