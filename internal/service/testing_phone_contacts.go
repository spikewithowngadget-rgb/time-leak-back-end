package service

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	appStoreTestPhone   = "+77471231213"
	appStoreTestOTPCode = "1111"
)

type TestingPhoneContact struct {
	Index int    `json:"index"`
	Email string `json:"email,omitempty"`
	Name  string `json:"name,omitempty"`
	Phone string `json:"phone"`
	Code  string `json:"code"`
}

var (
	//go:embed testing_phone_contacts.json
	testingPhoneContactsJSON []byte

	testingPhoneContacts      = mustLoadTestingPhoneContacts()
	testingPhoneCodesByNumber = buildTestingPhoneCodesByNumber(testingPhoneContacts)
)

func StaticTestingPhoneContacts() []TestingPhoneContact {
	contacts := make([]TestingPhoneContact, len(testingPhoneContacts))
	copy(contacts, testingPhoneContacts)
	return contacts
}

func lookupStaticTestingOTP(phone string) (string, bool) {
	phone = normalizePhone(phone)
	if phone == appStoreTestPhone {
		return appStoreTestOTPCode, true
	}

	code, ok := testingPhoneCodesByNumber[phone]
	return code, ok
}

func mustLoadTestingPhoneContacts() []TestingPhoneContact {
	var contacts []TestingPhoneContact
	if err := json.Unmarshal(testingPhoneContactsJSON, &contacts); err != nil {
		panic(fmt.Sprintf("load testing phone contacts: %v", err))
	}
	if err := validateTestingPhoneContacts(contacts); err != nil {
		panic(fmt.Sprintf("validate testing phone contacts: %v", err))
	}
	return contacts
}

func validateTestingPhoneContacts(contacts []TestingPhoneContact) error {
	if len(contacts) != 20 {
		return fmt.Errorf("expected 20 testing phone contacts, got %d", len(contacts))
	}

	seenPhones := make(map[string]struct{}, len(contacts))
	for i := range contacts {
		contacts[i].Email = strings.TrimSpace(contacts[i].Email)
		contacts[i].Name = strings.TrimSpace(contacts[i].Name)
		contacts[i].Phone = normalizePhone(contacts[i].Phone)
		contacts[i].Code = strings.TrimSpace(contacts[i].Code)

		if contacts[i].Index <= 0 {
			return fmt.Errorf("contact %d has invalid index", i)
		}
		if !phoneE164Pattern.MatchString(contacts[i].Phone) {
			return fmt.Errorf("contact %d has invalid phone %q", contacts[i].Index, contacts[i].Phone)
		}
		if !isValidOTPCode(contacts[i].Code) {
			return fmt.Errorf("contact %d has invalid code %q", contacts[i].Index, contacts[i].Code)
		}
		if _, ok := seenPhones[contacts[i].Phone]; ok {
			return fmt.Errorf("duplicate testing phone %q", contacts[i].Phone)
		}
		seenPhones[contacts[i].Phone] = struct{}{}

	}

	return nil
}

func buildTestingPhoneCodesByNumber(contacts []TestingPhoneContact) map[string]string {
	codes := make(map[string]string, len(contacts))
	for _, contact := range contacts {
		codes[contact.Phone] = contact.Code
	}
	return codes
}
