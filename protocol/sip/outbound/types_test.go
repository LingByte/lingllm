package outbound

import "testing"

func TestScenarioConstants(t *testing.T) {
	cases := []Scenario{
		ScenarioCampaign,
		ScenarioTransferAgent,
		ScenarioCallback,
		ScenarioManual,
	}
	for _, sc := range cases {
		if sc == "" {
			t.Fatal("scenario must be non-empty")
		}
	}
}

func TestDialEventStates(t *testing.T) {
	states := []string{
		DialEventInvited,
		DialEventProvisional,
		DialEventEstablished,
		DialEventFailed,
	}
	seen := make(map[string]bool)
	for _, s := range states {
		if s == "" {
			t.Fatal("empty dial event state")
		}
		if seen[s] {
			t.Fatalf("duplicate state %q", s)
		}
		seen[s] = true
	}
}
