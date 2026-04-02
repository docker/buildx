package kubeclient

import (
	"context"
	"net/http"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

type DeploymentClient interface {
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*appsv1.Deployment, error)
	Create(ctx context.Context, deployment *appsv1.Deployment, opts metav1.CreateOptions) (*appsv1.Deployment, error)
	Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error
}

type StatefulSetClient interface {
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*appsv1.StatefulSet, error)
	Create(ctx context.Context, deployment *appsv1.StatefulSet, opts metav1.CreateOptions) (*appsv1.StatefulSet, error)
	Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error
}

type ConfigMapClient interface {
	Create(ctx context.Context, configMap *corev1.ConfigMap, opts metav1.CreateOptions) (*corev1.ConfigMap, error)
	Update(ctx context.Context, configMap *corev1.ConfigMap, opts metav1.UpdateOptions) (*corev1.ConfigMap, error)
	Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error
}

type PodClient interface {
	List(ctx context.Context, opts metav1.ListOptions) (*corev1.PodList, error)
	RESTClient() rest.Interface
}

type Clients struct {
	Deployments  DeploymentClient
	StatefulSets StatefulSetClient
	ConfigMaps   ConfigMapClient
	Pods         PodClient
}

func New(config *rest.Config, namespace string) (*Clients, error) {
	httpClient, err := rest.HTTPClientFor(config)
	if err != nil {
		return nil, err
	}

	appsClient, err := newRESTClient(config, httpClient, appsv1.SchemeGroupVersion, "/apis")
	if err != nil {
		return nil, err
	}

	coreClient, err := newRESTClient(config, httpClient, corev1.SchemeGroupVersion, "/api")
	if err != nil {
		return nil, err
	}

	return &Clients{
		Deployments:  &deploymentClient{client: appsClient, namespace: namespace},
		StatefulSets: &statefulSetClient{client: appsClient, namespace: namespace},
		ConfigMaps:   &configMapClient{client: coreClient, namespace: namespace},
		Pods:         &podClient{client: coreClient, namespace: namespace},
	}, nil
}

func newRESTClient(config *rest.Config, httpClient *http.Client, gv schema.GroupVersion, apiPath string) (rest.Interface, error) {
	cfg := *config
	cfg.GroupVersion = &gv
	cfg.APIPath = apiPath
	cfg.NegotiatedSerializer = rest.CodecFactoryForGeneratedClient(Scheme(), Codecs()).WithoutConversion()
	if cfg.UserAgent == "" {
		cfg.UserAgent = rest.DefaultKubernetesUserAgent()
	}
	return rest.RESTClientForConfigAndClient(&cfg, httpClient)
}

type deploymentClient struct {
	client    rest.Interface
	namespace string
}

func (c *deploymentClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*appsv1.Deployment, error) {
	result := &appsv1.Deployment{}
	err := c.client.Get().
		UseProtobufAsDefault().
		Namespace(c.namespace).
		Resource("deployments").
		Name(name).
		VersionedParams(&opts, ParameterCodec()).
		Do(ctx).
		Into(result)
	return result, err
}

func (c *deploymentClient) Create(ctx context.Context, deployment *appsv1.Deployment, opts metav1.CreateOptions) (*appsv1.Deployment, error) {
	result := &appsv1.Deployment{}
	err := c.client.Post().
		UseProtobufAsDefault().
		Namespace(c.namespace).
		Resource("deployments").
		VersionedParams(&opts, ParameterCodec()).
		Body(deployment).
		Do(ctx).
		Into(result)
	return result, err
}

func (c *deploymentClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return c.client.Delete().
		UseProtobufAsDefault().
		Namespace(c.namespace).
		Resource("deployments").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

type statefulSetClient struct {
	client    rest.Interface
	namespace string
}

func (c *statefulSetClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*appsv1.StatefulSet, error) {
	result := &appsv1.StatefulSet{}
	err := c.client.Get().
		UseProtobufAsDefault().
		Namespace(c.namespace).
		Resource("statefulsets").
		Name(name).
		VersionedParams(&opts, ParameterCodec()).
		Do(ctx).
		Into(result)
	return result, err
}

func (c *statefulSetClient) Create(ctx context.Context, statefulSet *appsv1.StatefulSet, opts metav1.CreateOptions) (*appsv1.StatefulSet, error) {
	result := &appsv1.StatefulSet{}
	err := c.client.Post().
		UseProtobufAsDefault().
		Namespace(c.namespace).
		Resource("statefulsets").
		VersionedParams(&opts, ParameterCodec()).
		Body(statefulSet).
		Do(ctx).
		Into(result)
	return result, err
}

func (c *statefulSetClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return c.client.Delete().
		UseProtobufAsDefault().
		Namespace(c.namespace).
		Resource("statefulsets").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

type configMapClient struct {
	client    rest.Interface
	namespace string
}

func (c *configMapClient) Create(ctx context.Context, configMap *corev1.ConfigMap, opts metav1.CreateOptions) (*corev1.ConfigMap, error) {
	result := &corev1.ConfigMap{}
	err := c.client.Post().
		UseProtobufAsDefault().
		Namespace(c.namespace).
		Resource("configmaps").
		VersionedParams(&opts, ParameterCodec()).
		Body(configMap).
		Do(ctx).
		Into(result)
	return result, err
}

func (c *configMapClient) Update(ctx context.Context, configMap *corev1.ConfigMap, opts metav1.UpdateOptions) (*corev1.ConfigMap, error) {
	result := &corev1.ConfigMap{}
	err := c.client.Put().
		UseProtobufAsDefault().
		Namespace(c.namespace).
		Resource("configmaps").
		Name(configMap.Name).
		VersionedParams(&opts, ParameterCodec()).
		Body(configMap).
		Do(ctx).
		Into(result)
	return result, err
}

func (c *configMapClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return c.client.Delete().
		UseProtobufAsDefault().
		Namespace(c.namespace).
		Resource("configmaps").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

type podClient struct {
	client    rest.Interface
	namespace string
}

func (c *podClient) List(ctx context.Context, opts metav1.ListOptions) (*corev1.PodList, error) {
	result := &corev1.PodList{}
	err := c.client.Get().
		UseProtobufAsDefault().
		Namespace(c.namespace).
		Resource("pods").
		VersionedParams(&opts, ParameterCodec()).
		Do(ctx).
		Into(result)
	return result, err
}

func (c *podClient) RESTClient() rest.Interface {
	return c.client
}
