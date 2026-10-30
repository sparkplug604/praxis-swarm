package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestSwarmLegacyContractBytesAndHashRemainStable(t *testing.T) {
	const legacy = `{"id":"a","version":1,"objective":"existing task","oracleHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","agent":{"harness":"fake","model":"test","effort":"medium"},"dependencies":null,"resources":[{"namespace":"repo","kind":"path","key":"src"}],"reserveTokens":10}`
	var contract SwarmTaskContract
	if err := json.Unmarshal([]byte(legacy), &contract); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(contract)
	if err != nil || string(encoded) != legacy || contract.Work != nil {
		t.Fatalf("legacy serialization changed: %s, %v", encoded, err)
	}
	sum := sha256.Sum256([]byte(legacy))
	if SwarmHash(contract) != hex.EncodeToString(sum[:]) {
		t.Fatal("historical contract hash changed")
	}
}
