package auth
import (
	"net/http"
	"encoding/json"
	"bytes"
	"errors"
	"fmt"
)

type LoginRequest struct {
	Email string `json:"email"`
	Password string `json:"password"`
}


func AuthenticateAndGetNamespaces(authServerBaseUrl string, email string, password string) ([]string, error) {

	loginRequest := LoginRequest{Email: email, Password: password}
	loginRequestBody, err := json.Marshal(loginRequest)

	if err != nil {
		return nil, err
	}


	loginResponse, err := http.Post(authServerBaseUrl + "/auth/login", "application/json", bytes.NewBuffer(loginRequestBody))

	if err != nil {
		println("bla")
		return nil, err
	}

	cookies := loginResponse.Cookies()
	req, err := http.NewRequest("GET", authServerBaseUrl + "/rtiauth/namespaces", nil)

	if err != nil {
		println("bla2")

		return nil, err
	}

	for _, c := range cookies {
		req.AddCookie(c)
	}

	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, errors.New(fmt.Sprintf("Auth service returned invalid status code %v", resp.StatusCode))
	}


	body := resp.Body
	defer body.Close()

	decoder := json.NewDecoder(body)

	var namespaces = make([]string, 10)
	if err := decoder.Decode(&namespaces); err != nil {
		println("bla3")
		return nil, err
	}

	return namespaces, nil
}

func NameSpaceInSet(namespace string, namespaces []string) bool {
	for _, b := range namespaces {
		if b == namespace {
			return true
		}
	}
	return false
}