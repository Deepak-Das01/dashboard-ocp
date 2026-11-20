package controller

import (
	"fmt"

	"github.com/GehirnInc/crypt/apr1_crypt"
)

func apr1Htpasswd(user, password string) (string, error) {
	hash, err := apr1_crypt.New().Generate([]byte(password), nil)
	if err != nil {
		return "", fmt.Errorf("apr1 hash: %w", err)
	}
	return user + ":" + hash, nil
}
