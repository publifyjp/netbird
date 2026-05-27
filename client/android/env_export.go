package android

import (
	"os"

	log "github.com/sirupsen/logrus"
)

func exportEnvList(list *EnvList) {
	if list == nil {
		return
	}
	for k, v := range list.AllItems() {
		if err := os.Setenv(k, v); err != nil {
			log.Errorf("could not set env variable %s: %v", k, err)
		}
	}
}
