package promptcenter

import "encoding/json"

func BuildRenderSnapshot(input RenderSnapshot) (RenderSnapshot, error) {
	input.FinalHash = HashText(input.RenderedText)
	if input.ComponentsJSON == "" {
		payload, err := json.Marshal(input.Components)
		if err != nil {
			return RenderSnapshot{}, err
		}
		input.ComponentsJSON = string(payload)
	}
	return input, nil
}
