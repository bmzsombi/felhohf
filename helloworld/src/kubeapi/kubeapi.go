package kubeapi

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type KubeClient struct {
	Clientset kubernetes.Interface
}

func NewKubeClient() (*KubeClient, error) {
	secretName := "detector-kubeconfig"
	clusterConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(clusterConfig)
	if err != nil {
		return nil, err
	}

	secret, err := clientset.CoreV1().Secrets("detector").Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	kubeconfigData, ok := secret.Data["config"]
	if !ok {
		return nil, fmt.Errorf("secret %s/%s does not contain a 'config' key", "detector", secretName)
	}

	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	if err != nil {
		return nil, err
	}

	clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &KubeClient{Clientset: clientset}, nil
}

func generatePodName(jobId string) string {
	safeJobId := strings.ToLower(jobId)
	safeJobId = strings.ReplaceAll(safeJobId, "_", "-")
	var result strings.Builder
	for _, r := range safeJobId {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		}
	}
	name := result.String()
	if len(name) > 40 {
		name = name[:40]
	}
	name = strings.Trim(name, "-")
	if name == "" {
		name = "default-job"
	}
	return fmt.Sprintf("yolo-job-%s-%d", name, time.Now().UnixNano()%10000)
}

func (kc *KubeClient) CreatePod(filename string, pvcName string, namespace string) (string, error) {
	podName := generatePodName(filename)
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
		},
		Spec: v1.PodSpec{
			RestartPolicy: v1.RestartPolicyNever,
			Volumes: []v1.Volume{
				{
					Name: "image-storage",
					VolumeSource: v1.VolumeSource{
						PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
						},
					},
				},
			},
			Containers: []v1.Container{
				{
					Name:    "main-processor",
					Image:   "docker.io/ultralytics/yolov5:latest",
					Command: []string{"python3"},
					Args: []string{
						"detect.py",
						"--source", "/mnt/data/" + filename,
						"--project", "/mnt/data/",
						"--name", filename + "-detected",
						"--weights", "yolov5s.pt",
					},
					VolumeMounts: []v1.VolumeMount{
						{
							Name:      "image-storage",
							MountPath: "/mnt/data",
						},
					},
				},
			},
			SecurityContext: &v1.PodSecurityContext{
				RunAsUser:  new(int64), // 0 = root
				RunAsGroup: new(int64), // 0 = root
			},
		},
	}

	log.Printf("Attempting to create pod: %s in namespace: %s for image: %s", podName, namespace, filename)
	createdPod, err := kc.Clientset.CoreV1().Pods(namespace).Create(
		context.Background(),
		pod,
		metav1.CreateOptions{},
	)
	if err != nil {
		log.Printf("Cannot create pod '%s': %v", podName, err)
		return "", err
	}
	log.Printf("Successfully created pod: %s in namespace: %s", createdPod.Name, createdPod.Namespace)
	return createdPod.Name, nil
}
