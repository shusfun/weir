package contracts

import "testing"

func TestTaskErrorsRejectRawExternalMessages(t *testing.T) {
	valid := TaskError{Code: "laya_model_not_found", Origin: "laya.Client.ShowModel"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []TaskError{
		{Code: "https://example.com/private-token", Origin: "gate.runAction"},
		{Code: "upstream_failed", Message: "password=secret"},
		{Code: "upstream_failed", Origin: "Authorization: Bearer secret"},
	} {
		if bad.Validate() == nil {
			t.Fatalf("external text accepted: %+v", bad)
		}
	}
}
