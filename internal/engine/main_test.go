package engine

import (
	"os"
	"testing"

	"reviewparty/internal/hostrun"
)

func TestMain(m *testing.M) {
	hostrun.Init()
	os.Exit(m.Run())
}
