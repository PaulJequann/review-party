package settings

import (
	"encoding/json"
	"io"
)

type Settings struct {
	EmailNotifications bool `json:"email_notifications,omitempty"`
	DigestHour         int  `json:"digest_hour,omitempty"`
}

func Defaults() Settings {
	return Settings{EmailNotifications: true, DigestHour: 9}
}

func Load(reader io.Reader) (Settings, error) {
	settings := Defaults()
	err := json.NewDecoder(reader).Decode(&settings)
	return settings, err
}

func Save(writer io.Writer, settings Settings) error {
	return json.NewEncoder(writer).Encode(settings)
}
