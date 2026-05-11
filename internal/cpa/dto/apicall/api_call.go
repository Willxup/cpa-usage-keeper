package apicall

import "encoding/json"

type Request struct {
	AuthIndex string            `json:"authIndex"`
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Header    map[string]string `json:"header,omitempty"`
	Data      any               `json:"data,omitempty"`
}

type Response struct {
	StatusCode int             `json:"statusCode"`
	BodyText   string          `json:"bodyText"`
	Body       json.RawMessage `json:"body"`
}

func (r *Response) UnmarshalJSON(data []byte) error {
	type alias struct {
		StatusCode      int             `json:"statusCode"`
		BodyText        string          `json:"bodyText"`
		Body            json.RawMessage `json:"body"`
		StatusCodeSnake int             `json:"status_code"`
		BodyTextSnake   string          `json:"body_text"`
	}
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if decoded.StatusCode == 0 {
		decoded.StatusCode = decoded.StatusCodeSnake
	}
	if decoded.BodyText == "" {
		decoded.BodyText = decoded.BodyTextSnake
	}
	*r = Response{
		StatusCode: decoded.StatusCode,
		BodyText:   decoded.BodyText,
		Body:       decoded.Body,
	}
	return nil
}
