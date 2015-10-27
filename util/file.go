package util

import (
	"bufio"
	"os"
)

func FileExists(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	f.Close()

	return nil
}

func ParseMultiFileLinesToArray(envFiles []string) ([]string, error) {
	var envs []string

	for _, path := range envFiles {
		Log.Debugf("Parsing file %s", envFiles)
		parsedEnvs, err := ParseSingleFileLinesToArray(path)
		if err != nil {
			return nil, err
		}
		envs = append(envs, parsedEnvs...)
	}

	return envs, nil
}

func ParseSingleFileLinesToArray(path string) ([]string, error) {
	Log.Debugf("Parsing single file %s", path)
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}
