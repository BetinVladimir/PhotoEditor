package mail

import (
	"encoding/base64"
	"errors"
	"mime"
	"photoeditor/backend/internal/auth"
	"strings"
)

func encodeSubject(s string) string { return mime.QEncoding.Encode("UTF-8", s) }
func encodeBody(body string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	var b strings.Builder
	for len(encoded) > 76 {
		b.WriteString(encoded[:76])
		b.WriteString("\r\n")
		encoded = encoded[76:]
	}
	b.WriteString(encoded)
	b.WriteString("\r\n")
	return b.String()
}
func OTPMessage(recipient, id, locale, code string) (Message, error) {
	if !auth.ValidOTP(code) {
		return Message{}, errors.New("invalid OTP payload")
	}
	subject, body := "Your PhotoEditor sign-in code", "Your sign-in code: "+code+"\nThis code expires in 5 minutes. If you did not request it, ignore this message."
	switch locale {
	case "ru":
		subject = "Код входа в PhotoEditor"
		body = "Ваш код входа: " + code + "\nКод действует 5 минут. Если вы не запрашивали вход, проигнорируйте письмо."
	case "bg":
		subject = "Код за вход в PhotoEditor"
		body = "Вашият код за вход: " + code + "\nКодът е валиден 5 минути. Ако не сте поискали вход, игнорирайте съобщението."
	case "en":
	default:
		return Message{}, errors.New("invalid mail locale")
	}
	return Message{Recipient: recipient, Subject: subject, Body: body, ID: id}, nil
}

func InviteMessage(recipient, id, locale, tenant, token string) (Message, error) {
	if token == "" || tenant == "" {
		return Message{}, errors.New("invalid invitation payload")
	}
	subject := "Invitation to PhotoEditor"
	body := "You were invited to " + tenant + ".\nOpen PhotoEditor and enter this invitation token:\n" + token + "\nThe invitation expires in 7 days."
	switch locale {
	case "ru":
		subject = "Приглашение в PhotoEditor"
		body = "Вас пригласили в " + tenant + ".\nОткройте PhotoEditor и введите токен приглашения:\n" + token + "\nПриглашение действует 7 дней."
	case "bg":
		subject = "Покана за PhotoEditor"
		body = "Поканени сте в " + tenant + ".\nОтворете PhotoEditor и въведете токена за покана:\n" + token + "\nПоканата е валидна 7 дни."
	case "en":
	default:
		return Message{}, errors.New("invalid mail locale")
	}
	return Message{Recipient: recipient, Subject: subject, Body: body, ID: id}, nil
}

// RecentAuthMessage makes the sensitive purpose explicit and never calls it sign-in.
func RecentAuthMessage(recipient, id, locale, code, purpose string) (Message, error) {
	message, err := OTPMessage(recipient, id, locale, code)
	if err != nil {
		return Message{}, err
	}
	if purpose != "delete_profile" && purpose != "resolve_ownership" {
		return Message{}, errors.New("invalid recent auth mail purpose")
	}
	switch locale {
	case "en":
		message.Subject = "Confirm profile deletion in PhotoEditor"
		if purpose == "resolve_ownership" {
			message.Subject = "Confirm a studio ownership action in PhotoEditor"
		}
		message.Body = "Confirmation code: " + code + "\nThis code expires in 5 minutes and only authorizes this action. It cannot sign you in. If you did not request this action, ignore this message."
	case "ru":
		message.Subject = "Подтверждение удаления профиля PhotoEditor"
		if purpose == "resolve_ownership" {
			message.Subject = "Подтверждение действия с владельцем студии PhotoEditor"
		}
		message.Body = "Код подтверждения: " + code + "\nКод действует 5 минут и подходит только для этого действия. Для входа он не подходит. Если вы не запрашивали действие, проигнорируйте письмо."
	case "bg":
		message.Subject = "Потвърждение за изтриване на профил в PhotoEditor"
		if purpose == "resolve_ownership" {
			message.Subject = "Потвърждение за действие със собствеността на студио в PhotoEditor"
		}
		message.Body = "Код за потвърждение: " + code + "\nКодът е валиден 5 минути само за това действие. Не може да се използва за вход. Ако не сте поискали действието, игнорирайте съобщението."
	}
	return message, nil
}
