package msg

type ProfileType string

const (
	CPUProfileType  ProfileType = "CPU"
	HeapProfileType ProfileType = "HEAP"
)

type ProfilePostRequest struct {
	Profile    []byte              `json:"profile"`
	BinaryName string              `json:"binary_name"`
	BinaryMD5  string              `json:"binary_md5"`
	Kube       *KubeProfileRequest `json:"kube"`
}

type KubeProfileRequest struct {
	Namespace   string      `json:"namespace"`
	PodName     string      `json:"pod"`
	ProfileType ProfileType `json:"profile_type"`
}

type ProfilePostResponse struct {
	ID string `json:"id"`
}

type ProfileListResponse struct {
	Profiles []ProfileInfo `json:"profiles"`
}

type ProfileInfo struct {
	ID string `json:"id"`
}
