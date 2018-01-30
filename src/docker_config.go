package main

type DockerConfigEntry struct {
	Server string        `json:"-"`
	Auth string          `json:"auth,omitempty"`
	Identitytoken string `json:"identitytoken,omitempty"`
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
