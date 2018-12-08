package msg

type ProfilePostRequest struct {
	Profile    []byte              `json:"profile"`
	BinaryName string              `json:"binary_name"`
	BinaryMD5  string              `json:"binary_md5"`
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
