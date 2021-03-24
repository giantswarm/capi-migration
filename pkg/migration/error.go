package migration

import (
	"github.com/Azure/go-autorest/autorest"
	"github.com/giantswarm/microerror"
)

var newMasterNotReadyError = &microerror.Error{
	Kind: "newMasterNotReadyError",
}

var tooManyMastersError = &microerror.Error{
	Kind: "tooManyMastersError",
}

// IsAzureNotFound detects an azure API 404 error.
func IsAzureNotFound(err error) bool {
	if err == nil {
		return false
	}

	c := microerror.Cause(err)

	{
		dErr, ok := c.(autorest.DetailedError)
		if ok {
			if dErr.StatusCode == 404 {
				return true
			}
		}
	}

	return false
}
