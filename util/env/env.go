package env

import "os"

func getEnv(key string) (string, bool) {
	return os.LookupEnv(key)
}

func GetAccessToken() (string, bool) {
	return getEnv("ACCESS_TOKEN")
}
