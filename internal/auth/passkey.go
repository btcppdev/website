package auth

import (
	"encoding/json"
	"errors"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
)

type PasskeyUser struct {
	PersonID    string
	Name        string
	Email       string
	Credentials []webauthn.Credential
}

func LoadPasskeyUser(ctx *config.AppContext, personID string) (*PasskeyUser, error) {
	person, err := getters.FetchSpeakerByID(ctx, personID)
	if err != nil || person == nil {
		if err == nil {
			err = errors.New("passkey person does not exist")
		}
		return nil, err
	}
	email, err := getters.GetPrimaryPersonEmail(ctx, personID)
	if err != nil {
		return nil, err
	}
	stored, err := getters.ListPersonPasskeyCredentials(ctx, personID)
	if err != nil {
		return nil, err
	}
	user := &PasskeyUser{PersonID: personID, Name: person.Name, Email: email}
	for _, item := range stored {
		var credential webauthn.Credential
		if err := json.Unmarshal(item.CredentialJSON, &credential); err != nil {
			return nil, err
		}
		user.Credentials = append(user.Credentials, credential)
	}
	return user, nil
}

func (u *PasskeyUser) WebAuthnID() []byte {
	id, err := uuid.Parse(u.PersonID)
	if err != nil {
		return nil
	}
	return id[:]
}

func (u *PasskeyUser) WebAuthnName() string {
	if u.Email != "" {
		return u.Email
	}
	return u.Name
}

func (u *PasskeyUser) WebAuthnDisplayName() string { return u.Name }

func (u *PasskeyUser) WebAuthnCredentials() []webauthn.Credential { return u.Credentials }
