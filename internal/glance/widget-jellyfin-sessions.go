package glance

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"time"
)

var jellyfinSessionsWidgetTemplate = mustParseTemplate("jellyfin-sessions.html", "widget-base.html")

type jellyfinSessionsWidget struct {
	widgetBase          `yaml:",inline"`
	JellyfinURL         string            `yaml:"url"`
	JellyfinApiKey      string            `yaml:"api-key"`
	HideUndefinedUsers  bool              `yaml:"hide-undefined-users"`
	DefinedDisplayNames map[string]string `yaml:"display-names"`
	CollapseAfter       int               `yaml:"collapse-after"`
	Sessions            []jellyfinSession `yaml:"-"`
}

type jellyfinSession struct {
	DisplayName     string
	DeviceName      string
	Client          string
	UserId          string
	IsNowPlaying    bool
	PlayMethod      string
	NowPlaying      string
	Runtime         string
	RuntimeProgress int
	MediaId         string
}

type jellyfinSessionList []jellyfinSession

func (widget *jellyfinSessionsWidget) initialize() error {
	widget.
		withTitle("Jellyfin Sessions").
		withTitleURL(widget.JellyfinURL).
		withCacheDuration(0)

	if widget.CollapseAfter == 0 || widget.CollapseAfter < -1 {
		widget.CollapseAfter = 5
	}

	if widget.JellyfinURL == "" || widget.JellyfinApiKey == "" {
		return fmt.Errorf("missing Jellyfin URL or API key")
	}

	if widget.JellyfinURL[len(widget.JellyfinURL)-1] != '/' {
		widget.JellyfinURL += "/"
	}

	return nil
}

func (widget *jellyfinSessionsWidget) update(ctx context.Context) {
	sessions, err := fetchSessionsFromJellyfin(widget)
	if err != nil {
		widget.withError(err)
		return
	}

	if !widget.canContinueUpdateAfterHandlingErr(err) {
		return
	}

	widget.Sessions = sessions
}

func (widget *jellyfinSessionsWidget) Render() template.HTML {
	return widget.renderTemplate(widget, jellyfinSessionsWidgetTemplate)
}

type PlayState struct {
	PositionTicks int64  `json:"PositionTicks,omitempty"`
	PlayMethod    string `json:"PlayMethod,omitempty"`
	MediaSourceId string `json:"MediaSourceId,omitempty"`
}

type NowPlayingItem struct {
	Name         string `json:"Name"`
	Type         string `json:"Type"`
	SeasonName   string `json:"SeasonName"`
	SeriesName   string `json:"SeriesName"`
	RuntimeTicks int64  `json:"RuntimeTicks"`
}

type jellyfinSessionsResponse struct {
	PlayState      PlayState      `json:"PlayState"`
	UserName       string         `json:"UserName"`
	Client         string         `json:"Client"`
	DeviceName     string         `json:"DeviceName"`
	UserId         string         `json:"UserId"`
	NowPlayingItem NowPlayingItem `json:"NowPlayingItem"`
}

const jellyfinSessionsEndpoint = "Sessions?ActiveWithinSeconds=960"
const jellyfinAuthorizationFmt = `MediaBrowser Token="%s"`

func fetchSessionsFromJellyfin(widget *jellyfinSessionsWidget) (jellyfinSessionList, error) {
	result := make(jellyfinSessionList, 0)

	jellyfinSessionsUrl := widget.JellyfinURL + jellyfinSessionsEndpoint
	jellyfinAuthorizaion := fmt.Sprintf(jellyfinAuthorizationFmt, widget.JellyfinApiKey)

	request, _ := http.NewRequest("GET", jellyfinSessionsUrl, nil)
	request.Header.Add("Authorization", jellyfinAuthorizaion)

	response, err := decodeJsonFromRequest[[]jellyfinSessionsResponse](defaultHTTPClient, request)
	if err != nil {
		return result, fmt.Errorf("fetching Jellyfin sessions: %w", err)
	}

	for i := range response {
		displayName, ok := widget.DefinedDisplayNames[response[i].UserName]
		if !ok && widget.HideUndefinedUsers {
			// Skipping undefined user
			continue
		}

		if !ok {
			displayName = response[i].UserName
		}

		isNowPlaying := response[i].NowPlayingItem.Name != ""
		runtimeProgress := 0
		nowPlaying := ""
		runtime := ""

		if isNowPlaying {
			runtimeProgress = int(float64(response[i].PlayState.PositionTicks) / float64(response[i].NowPlayingItem.RuntimeTicks) * 100)
			// Convert PositionTicks (100-nanosecond intervals) to h:m:s
			positionTime := time.Duration(response[i].PlayState.PositionTicks * 100) // Convert to nanoseconds
			runtime = fmt.Sprintf("%02d:%02d:%02d", int(positionTime.Hours()), int(positionTime.Minutes())%60, int(positionTime.Seconds())%60)
		}

		switch response[i].NowPlayingItem.Type {
		case "Episode":
			nowPlaying = fmt.Sprintf("%s - %s (%s)", response[i].NowPlayingItem.SeriesName, response[i].NowPlayingItem.Name, response[i].NowPlayingItem.SeasonName)
		case "Movie":
			nowPlaying = response[i].NowPlayingItem.Name
		default:
			nowPlaying = response[i].NowPlayingItem.Name
		}

		session := jellyfinSession{
			DisplayName:     displayName,
			DeviceName:      response[i].DeviceName,
			Client:          response[i].Client,
			UserId:          response[i].UserId,
			IsNowPlaying:    isNowPlaying,
			PlayMethod:      response[i].PlayState.PlayMethod,
			NowPlaying:      nowPlaying,
			Runtime:         runtime,
			RuntimeProgress: runtimeProgress,
			MediaId:         response[i].PlayState.MediaSourceId,
		}

		result = append(result, session)
	}

	return result, nil
}
