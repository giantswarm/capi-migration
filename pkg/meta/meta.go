package meta

var (
	Annotation AnnotationType
	Label      LabelType
)

type AnnotationType struct {
}

type LabelType struct {
	// Version is standard "capi-migration.giantswarm.io/version" label.
	Version
}
