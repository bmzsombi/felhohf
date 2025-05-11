package kubeapi

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type KubeClient struct {
	Clientset kubernetes.Interface
}

func NewKubeClient() (*KubeClient, error) {
	var kubeconfig string
	if os.Getenv("KUBECONFIG") != "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &KubeClient{Clientset: clientset}, nil
}

func (kc *KubeClient) CreatePod(filename string, pvcName string, namespace string) (string, error) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "yolopod-" + filename,
			Namespace: namespace,
		},
		Spec: v1.PodSpec{
			RestartPolicy: v1.RestartPolicyNever, // Job-szerű Pod, nem indul újra automatikusan
			Volumes: []v1.Volume{ // Itt definiáljuk a Pod által használt volume-okat
				{
					Name: "image-storage", // A volume neve a Pod-on belül
					VolumeSource: v1.VolumeSource{
						PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName, // A Kubernetes klaszterben létező PVC neve
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
					VolumeMounts: []v1.VolumeMount{ // Itt csatoljuk a volume-ot a konténerhez
						{
							Name:      "image-storage", // A fent definiált Volume neve
							MountPath: "/mnt/data",     // Hova csatolja a konténeren belül
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

	log.Printf("Attempting to create pod: %s in namespace: %s for image: %s", "yolopod-"+filename, namespace, filename)
	createdPod, err := kc.Clientset.CoreV1().Pods(namespace).Create(
		context.Background(),
		pod,
		metav1.CreateOptions{},
	)
	if err != nil {
		log.Printf("Cannot create pod '%s': %v", "yolopod-"+filename, err)
		return "", err
	}
	log.Printf("Successfully created pod: %s in namespace: %s", createdPod.Name, createdPod.Namespace)
	return createdPod.Name, nil
}

func (kc *KubeClient) WaitForPodCompletion(podName, namespace string, timeout time.Duration) error {
	log.Printf("Waiting for pod %s to complete...", podName)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for pod %s to complete: %w", podName, ctx.Err())
		default:
			pod, err := kc.Clientset.CoreV1().Pods(namespace).Get(context.Background(), podName, metav1.GetOptions{})
			if err != nil {
				log.Printf("Error getting pod %s status: %v. Retrying...", podName, err)
				// Lehet, hogy a pod még nem jelenik meg azonnal a lekérdezéskor
				time.Sleep(2 * time.Second)
				continue
			}

			log.Printf("Pod %s status: %s", podName, pod.Status.Phase)

			switch pod.Status.Phase {
			case v1.PodSucceeded:
				log.Printf("Pod %s completed successfully.", podName)
				return nil
			case v1.PodFailed:
				// Próbáljuk meg lekérni a konténer logját hiba esetén
				containerLog, logErr := kc.GetPodLogs(podName, namespace, "yolo-processor")
				if logErr != nil {
					log.Printf("Error getting logs for failed pod %s: %v", podName, logErr)
				} else {
					log.Printf("Logs for failed pod %s:\n%s", podName, containerLog)
				}
				return fmt.Errorf("pod %s failed. Status: %s, Message: %s", podName, pod.Status.Reason, pod.Status.Message)
			case v1.PodPending, v1.PodRunning:
				// Várjunk tovább
				time.Sleep(5 * time.Second) // Poll interval
			default:
				// Ismeretlen vagy átmeneti állapot
				time.Sleep(5 * time.Second)
			}
		}
	}
}

func (kc *KubeClient) GetPodLogs(podName, namespace, containerName string) (string, error) {
	podLogOpts := v1.PodLogOptions{
		Container: containerName,
	}
	req := kc.Clientset.CoreV1().Pods(namespace).GetLogs(podName, &podLogOpts)
	podLogs, err := req.Stream(context.Background())
	if err != nil {
		return "", fmt.Errorf("error in opening stream: %w", err)
	}
	defer podLogs.Close()

	buf := new(strings.Builder)
	_, err = io.Copy(buf, podLogs)
	if err != nil {
		return "", fmt.Errorf("error in copy information from log stream: %w", err)
	}
	return buf.String(), nil
}
