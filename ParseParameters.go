package gojsonrpc2server

import (
	"encoding/json"
)

// responder - can be nil - so will not be used
func ParseParameters(
	responder *HandleResponder,
	params *json.RawMessage,
	v interface{},
) (
	cancel_processing bool,
	paniced bool,
) {

	cancel_processing = true

	defer func() {
		if x := recover(); x != nil {
			responder.Log("run time panic while processing request: %v", x)
			paniced = true
		}
	}()

	// TODO: this can crush if params wasn't defined in request
	data, err := params.MarshalJSON()
	if err != nil {
		if responder != nil {
			err2 := responder.LogRespError(500, "error", "can't get input data for unmarshal")
			if err != nil {
				responder.Log("can't send error message to caller:", err2, "about:", err)
			}
		}
		return
	}

	err = json.Unmarshal(data, v)
	if err != nil {
		if responder != nil {
			err2 := responder.LogRespError(500, "error", "can't unmarshal input data")
			if err != nil {
				responder.Log("can't send error message to caller:", err2, "about:", err)
			}
		}
		return
	}

	cancel_processing = false

	return
}
