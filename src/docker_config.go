package main

type DockerConfigEntry struct {
	Auth string          `json:"auth"`
	Identitytoken string `json:"identitytoken"`
}

type DockerConfig struct {
	Auths map[string]DockerConfigEntry `json:"auths"`
}

func CreateDockerConfig() (conf *DockerConfig){
	conf = &DockerConfig{}
	conf.Auths = map[string]DockerConfigEntry{}
	return
}
