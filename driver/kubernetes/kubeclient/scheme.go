package kubeclient

import (
	"sync"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

type schemeData struct {
	scheme         *runtime.Scheme
	codecs         serializer.CodecFactory
	parameterCodec runtime.ParameterCodec
}

var loadSchemeData = sync.OnceValue(func() schemeData {
	s := runtime.NewScheme()
	metav1.AddToGroupVersion(s, schema.GroupVersion{Version: "v1"})
	utilruntime.Must(corev1.AddToScheme(s))
	utilruntime.Must(appsv1.AddToScheme(s))

	return schemeData{
		scheme:         s,
		codecs:         serializer.NewCodecFactory(s),
		parameterCodec: runtime.NewParameterCodec(s),
	}
})

func Scheme() *runtime.Scheme {
	return loadSchemeData().scheme
}

func Codecs() serializer.CodecFactory {
	return loadSchemeData().codecs
}

func ParameterCodec() runtime.ParameterCodec {
	return loadSchemeData().parameterCodec
}
