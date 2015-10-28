package util

import "strings"

func MaskEnv(unmaskedEnvs []string) []string {
	var maskedEnvs []string
	for _, val := range unmaskedEnvs {
		kv := strings.Split(val, "=")
		if strings.Contains(kv[0], "pass") {
			maskedEnvs = append(maskedEnvs, kv[0]+"="+"*****")
		} else {
			maskedEnvs = append(maskedEnvs, val)
		}
	}

	return maskedEnvs
}
