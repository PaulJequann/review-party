package store

import (
	"database/sql"
	"errors"
	"testing"

	"reviewparty/internal/model"
)

func TestLedgerReportsUnknownReviewAndBundleIDs(t *testing.T) {
	ledger, err := NewLedgerRecordStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, ledger.Close)

	_, recordErr := ledger.Load("rp_1724232000000_0123456789abcdef")
	_, bundleErr := ledger.LoadReviewBundle("rb_1724232000000_0123456789abcdef")
	for id, err := range map[string]error{"rp_1724232000000_0123456789abcdef": recordErr, "rb_1724232000000_0123456789abcdef": bundleErr} {
		if !errors.Is(err, ErrReviewNotFound) {
			t.Fatalf("%s: err = %v, want ErrReviewNotFound", id, err)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("%s: err = %v, want the wrapped sql.ErrNoRows", id, err)
		}
		if want := `no review with id "` + id + `"`; err.Error() != want {
			t.Fatalf("%s: message = %q, want %q", id, err.Error(), want)
		}
	}
}

func TestDeferredLedgerReportsUnknownReviewID(t *testing.T) {
	directory := t.TempDir()
	if err := PrepareReviewRecordState(directory); err != nil {
		t.Fatal(err)
	}
	deferred, err := NewDeferredLedgerRecordStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	_, err = deferred.Load(model.ReviewID("rp_1724232000000_0123456789abcdef"))
	if !errors.Is(err, ErrReviewNotFound) {
		t.Fatalf("err = %v, want ErrReviewNotFound", err)
	}
}
