package main

type DockerConfigEntry struct {
	Server string        `json:"-"`
	Auth string          `json:"auth"`
	Identitytoken string `json:"identitytoken"`
	ValidUntil int64     `json:"-"`

}

type DockerConfig struct {
	Auths map[string]DockerConfigEntry `json:"auths"`
}

func CreateDockerConfig() (conf *DockerConfig){
	conf = &DockerConfig{}
	conf.Auths = map[string]DockerConfigEntry{}
	return
}
