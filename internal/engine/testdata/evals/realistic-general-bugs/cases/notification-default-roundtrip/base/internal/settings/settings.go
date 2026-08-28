package settings

import (
	"encoding/json"
	"io"
)

type Settings struct {
	EmailNotifications bool `json:"email_notifications"`
}

func Defaults() Settings {
	return Settings{EmailNotifications: true}
}

func Load(reader io.Reader) (Settings, error) {
	settings := Defaults()
	err := json.NewDecoder(reader).Decode(&settings)
	return settings, err
}

func Save(writer io.Writer, settings Settings) error {
	return json.NewEncoder(writer).Encode(settings)
}
