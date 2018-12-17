package kube

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/cgilling/pprof-me/reqproxy"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const DefaultAppNameLabel = "app"

type Config struct {
	InCluster      bool   `envconfig:"IN_CLUSTER"`
	ConfigPath     string `envconfig:"CONFIG_PATH"`
	PodLabelFilter string `envconfig:"POD_LABEL_FILTER"`
	Namespace      string `envconfig:"NAMESPACE"`
	AppNameLabel   string `envconfig:"APP_NAME_LABEL"`
}

type PodProvider struct {
	config    Config
	clientset *kubernetes.Clientset
}

type Pod struct {
	AppName string
	*corev1.Pod
}

func NewPodProvider(c Config) (*PodProvider, error) {
	var err error
	var kubeConfig *rest.Config
	if c.InCluster {
		kubeConfig, err = rest.InClusterConfig()
	} else {
		kubeConfig, err = clientcmd.BuildConfigFromFlags("", c.ConfigPath)
	}
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}
	if c.AppNameLabel == "" {
		c.AppNameLabel = DefaultAppNameLabel
	}
	return &PodProvider{
		config:    c,
		clientset: clientset,
	}, nil
}

func (pp *PodProvider) GetPods() ([]Pod, error) {
	options := metav1.ListOptions{
		LabelSelector: pp.config.PodLabelFilter,
	}
	pods, err := pp.clientset.CoreV1().Pods(pp.config.Namespace).List(options)
	if err != nil {
		return nil, err
	}
	var retVal []Pod
	for _, pod := range pods.Items {
		pod := pod
		myPod := Pod{Pod: &pod}
		myPod.fillInAppName(pp.config.AppNameLabel)
		retVal = append(retVal, myPod)
	}
	return retVal, nil
}

func (p *Pod) fillInAppName(label string) {
	if val, ok := p.ObjectMeta.Labels[label]; ok {
		p.AppName = val
		return
	}
	imageName := p.Spec.Containers[0].Image
	p.AppName = strings.Split(filepath.Base(imageName), ":")[0]
}

type KubeAPIProxy struct {
	clientset *kubernetes.Clientset
	pod       Pod
	path      string
}

func (kap *KubeAPIProxy) String() string {
	return fmt.Sprintf("{namespace: %v, pod: %v, path: %v}",
		kap.pod.ObjectMeta.Name, kap.pod.ObjectMeta.Namespace, kap.path)
}

func (kap *KubeAPIProxy) ProxyAndReturnBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	req := kap.clientset.CoreV1().RESTClient().Get().
		Namespace(kap.pod.ObjectMeta.Namespace).
		Resource("pods").
		Name(kap.pod.ObjectMeta.Name).
		Suffix("proxy", kap.path).
		Timeout(time.Minute)
	fmt.Println(req.URL().String())
	b, err := req.DoRaw()
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "failed to proxy request: %v", err)
		fmt.Println(err)
		return nil, err
	}
	fmt.Println("successfully proxied request")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(b)
	return b, err
}

func (pp *PodProvider) NewProxy(pod Pod, path string) reqproxy.RequestProxy {
	return &KubeAPIProxy{
		clientset: pp.clientset,
		pod:       pod,
		path:      path,
	}
}
