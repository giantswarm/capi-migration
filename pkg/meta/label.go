package meta

import (
	"github.com/giantswarm/capi-migration/pkg/project"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

var (
	versionLabel = project.Name() + ".giantswarm.io/version"
)

type Version struct{}

func (Version) Key() string { return versionLabel }

func (Version) Val() string { return project.Version() }

func (Version) Predicate(meta metav1.Object, object runtime.Object) bool {
	if len(meta.GetLabels()) == 0 {
		return false
	}

	v, ok := meta.GetLabels()[Version{}.Key()]
	if !ok {
		return false
	}

	return v == Version{}.Val()
}
