package livetv

import (
	"context"
	"fmt"
	"strings"
)

// Imported Stremio streams may repeat the add-on's credentials in their
// URLs. The owner can reorder and disable them without receiving those URLs.
func (s *Service) redactAddonChannels(_ context.Context, channels []Channel) {
	for i := range channels {
		c := &channels[i]
		for j := range c.StreamSources {
			input := &c.StreamSources[j]
			if strings.HasPrefix(input.ID, "stremio-") {
				if c.StreamURL == input.URL {
					c.StreamURL = redactedSecret
				}
				input.URL = redactedSecret
			}
		}
	}
}
func (s *Service) restoreAddonChannel(ctx context.Context, c *Channel) error {
	needs := c.StreamURL == redactedSecret
	for _, v := range c.StreamSources {
		needs = needs || v.URL == redactedSecret
	}
	if !needs {
		return nil
	}
	old, err := s.Channel(ctx, c.ID)
	if err != nil || old.SourceID != c.SourceID {
		return fmt.Errorf("re-enter the stream URL")
	}
	src, err := s.storedSourceByID(ctx, old.SourceID)
	if err != nil || src.Kind != "stremio" {
		return fmt.Errorf("re-enter the stream URL")
	}
	for i := range c.StreamSources {
		input := &c.StreamSources[i]
		if input.URL != redactedSecret {
			continue
		}
		found := false
		for _, v := range old.StreamSources {
			if v.ID == input.ID && strings.HasPrefix(v.ID, "stremio-") {
				input.URL = v.URL
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("re-enter the stream URL")
		}
	}
	if c.StreamURL == redactedSecret {
		c.StreamURL = old.StreamURL
	}
	return nil
}
