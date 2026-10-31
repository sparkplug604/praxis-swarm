package planning

import "testing"

func TestDiscoveryStrictJSON(t *testing.T) {
	for _, raw := range []string{`{"expectedRevision":0,"brief":{"goal":"Build"}}`, `{"expectedRevision":3,"brief":{"questions":[]}}`} {
		if _, err := ParseDiscoveryUpdate([]byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{`null`, `{"expectedRevision":null}`, `{"ExpectedRevision":0}`, `{"expectedRevision":0,"expectedRevision":1}`, `{"brief":{"goal":"One","goal":"Two"}}`, `{"brief":{"unknown":true}}`, `{} {}`, `{"expectedRevision":1.5}`, `{"brief":{"questions":[{"id":"x","question":"Q?","required":true,"answer":null}]}}`} {
		if _, err := ParseDiscoveryUpdate([]byte(raw)); err == nil {
			t.Fatal("accepted", raw)
		}
	}
}
