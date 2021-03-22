package migration

import "github.com/giantswarm/microerror"

var podNotSucceededError = &microerror.Error{
	Kind: "podNotSucceededError",
}
