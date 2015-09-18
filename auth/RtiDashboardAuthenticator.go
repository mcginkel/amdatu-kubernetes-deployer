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

type Member struct {
	Roles []string `json:"roles"`
}

type Namespaces []string

func AuthenticateAndGetNamespaces(authServerBaseUrl string, email string, password string) (Namespaces, error) {

	loginRequest := LoginRequest{Email: email, Password: password}
	loginRequestBody, err := json.Marshal(loginRequest)

	if err != nil {
		return nil, err
	}

	loginResponse, err := http.Post(authServerBaseUrl + "/auth/login", "application/json", bytes.NewBuffer(loginRequestBody))

	if err != nil {
		return nil, err
	}

	member, err := decodeMember(loginResponse)

	if err != nil {
		return nil, err
	}

	if !StringInSet("DEPLOYER", member.Roles)  {
		fmt.Printf("Roles: %v", member)
		return nil, errors.New("Member doesn't have role 'DEPLOYER'")
	}

	cookies := loginResponse.Cookies()
	req, err := http.NewRequest("GET", authServerBaseUrl + "/rtiauth/namespaces", nil)

	if err != nil {
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

	namespaces, err := decodeNamespaces(resp)

	if err != nil {
		return nil, err
	}

	return namespaces, nil
}

func decodeMember(resp *http.Response) (Member, error) {
	body := resp.Body
	defer body.Close()

	decoder := json.NewDecoder(body)

	member := Member{}
	if err := decoder.Decode(&member); err != nil {
		return Member{}, err
	}

	return member, nil
}

func decodeNamespaces(resp *http.Response) ([]string, error) {
	body := resp.Body
	defer body.Close()

	decoder := json.NewDecoder(body)

	namespaces := make([]string, 10)
	if err := decoder.Decode(&namespaces); err != nil {
		return nil, err
	}

	return namespaces, nil
}

func StringInSet(stringToFind string, strings []string) bool {
	for _, b := range strings {
		if b == stringToFind {
			return true
		}
	}
	return false
}