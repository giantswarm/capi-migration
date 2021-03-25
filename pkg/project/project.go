package project

var (
	description = "Controllers transforming GS clusters into CAPI clusters."
	gitSHA      = "n/a"
	name        = "capi-migration"
	source      = "https://github.com/giantswarm/capi-migration"
	version     = "0.1.0-dev"
)

func Description() string {
	return description
}

func GitSHA() string {
	return gitSHA
}

func Name() string {
	return name
}

func Source() string {
	return source
}

func Version() string {
	return version
}
