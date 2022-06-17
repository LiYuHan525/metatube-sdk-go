package heyzo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/gocolly/colly/v2"

	"github.com/javtube/javtube-sdk-go/common/m3u8"
	"github.com/javtube/javtube-sdk-go/common/parser"
	"github.com/javtube/javtube-sdk-go/model"
	"github.com/javtube/javtube-sdk-go/provider"
	"github.com/javtube/javtube-sdk-go/provider/internal/scraper"
)

var _ provider.MovieProvider = (*Heyzo)(nil)

const (
	Name     = "HEYZO"
	Priority = 1000
)

const (
	baseURL   = "https://www.heyzo.com/"
	movieURL  = "https://www.heyzo.com/moviepages/%04s/index.html"
	sampleURL = "https://www.heyzo.com/contents/%s/%s/%s"
)

type Heyzo struct {
	*scraper.Scraper
}

func New() *Heyzo {
	return &Heyzo{scraper.NewDefaultScraper(Name, baseURL, Priority)}
}

func (hzo *Heyzo) NormalizeID(id string) string {
	if ss := regexp.MustCompile(`^(?i)(?:heyzo-)?(\d+)$`).FindStringSubmatch(id); len(ss) == 2 {
		return ss[1]
	}
	return ""
}

func (hzo *Heyzo) GetMovieInfoByID(id string) (info *model.MovieInfo, err error) {
	return hzo.GetMovieInfoByURL(fmt.Sprintf(movieURL, id))
}

func (hzo *Heyzo) ParseIDFromURL(rawURL string) (string, error) {
	homepage, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return path.Base(path.Dir(homepage.Path)), nil
}

func (hzo *Heyzo) GetMovieInfoByURL(rawURL string) (info *model.MovieInfo, err error) {
	id, err := hzo.ParseIDFromURL(rawURL)
	if err != nil {
		return
	}

	info = &model.MovieInfo{
		ID:            id,
		Number:        fmt.Sprintf("HEYZO-%s", id),
		Provider:      hzo.Name(),
		Homepage:      rawURL,
		Maker:         "HEYZO",
		Actors:        []string{},
		PreviewImages: []string{},
		Tags:          []string{},
	}

	c := hzo.ClonedCollector()

	// JSON
	c.OnXML(`//script[@type="application/ld+json"]`, func(e *colly.XMLElement) {
		data := struct {
			Name          string `json:"name"`
			Image         string `json:"image"`
			Description   string `json:"description"`
			ReleasedEvent struct {
				StartDate string `json:"startDate"`
			} `json:"releasedEvent"`
			Video struct {
				Duration string `json:"duration"`
				Actor    string `json:"actor"`
				Provider string `json:"provider"`
			} `json:"video"`
			AggregateRating struct {
				RatingValue string `json:"ratingValue"`
			} `json:"aggregateRating"`
		}{}
		if json.Unmarshal([]byte(e.Text), &data) == nil {
			info.Title = data.Name
			info.Summary = data.Description
			info.CoverURL = e.Request.AbsoluteURL(data.Image)
			info.ThumbURL = info.CoverURL /* use cover as thumb */
			info.ReleaseDate = parser.ParseDate(data.ReleasedEvent.StartDate)
			info.Runtime = parser.ParseRuntime(data.Video.Duration)
			info.Score = parser.ParseScore(data.AggregateRating.RatingValue)
			if data.Video.Provider != "" {
				info.Maker = data.Video.Provider
			}
			if data.Video.Actor != "" {
				info.Actors = []string{data.Video.Actor}
			}
		}
	})

	// Title
	c.OnXML(`//*[@id="movie"]/h1`, func(e *colly.XMLElement) {
		if info.Title == "" {
			info.Title = strings.Fields(e.Text)[0]
		}
	})

	// Summary
	c.OnXML(`//p[@class="memo"]`, func(e *colly.XMLElement) {
		if info.Summary == "" {
			info.Summary = strings.TrimSpace(e.Text)
		}
	})

	// Thumb+Cover (fallback)
	c.OnXML(`//meta[@property="og:image"]`, func(e *colly.XMLElement) {
		if info.CoverURL == "" {
			info.CoverURL = e.Request.AbsoluteURL(e.Attr("content"))
			info.ThumbURL = info.CoverURL
		}
	})

	// Fields
	c.OnXML(`//table[@class="movieInfo"]/tbody/tr`, func(e *colly.XMLElement) {
		switch e.ChildText(`.//td[1]`) {
		case "公開日":
			info.ReleaseDate = parser.ParseDate(e.ChildText(`.//td[2]`))
		case "出演":
			info.Actors = e.ChildTexts(`.//td[2]/a/span`)
		case "シリーズ":
			info.Series = strings.Trim(e.ChildText(`.//td[2]`), "-")
		case "評価":
			info.Score = parser.ParseScore(e.ChildText(`.//span[@itemprop="ratingValue"]`))
		}
	})

	// Tags
	c.OnXML(`//ul[@class="tag-keyword-list"]`, func(e *colly.XMLElement) {
		info.Tags = e.ChildTexts(`.//li/a`)
	})

	// Video+Runtime
	c.OnXML(`//script[@type="text/javascript"]`, func(e *colly.XMLElement) {
		// Sample Video
		if strings.Contains(e.Text, "emvideo") {
			if sub := regexp.MustCompile(`emvideo = "(.+?)";`).FindStringSubmatch(e.Text); len(sub) == 2 {
				info.PreviewVideoURL = e.Request.AbsoluteURL(sub[1])
			}
		}
		// Runtime
		if strings.Contains(e.Text, "o = {") {
			if sub := regexp.MustCompile(`o = (\{.+?});`).FindStringSubmatch(e.Text); len(sub) == 2 {
				data := struct {
					Full string `json:"full"`
				}{}
				if json.Unmarshal([]byte(sub[1]), &data) == nil {
					info.Runtime = parser.ParseRuntime(data.Full)
				}
			}
		}
	})

	// Preview Video
	c.OnXML(`//*[@id="playerContainer"]/script`, func(e *colly.XMLElement) {
		if !strings.Contains(e.Text, "movieId") {
			return
		}
		var movieID, siteID string
		if sub := regexp.MustCompile(`movieId\s*=\s*'(\d+?)';`).FindStringSubmatch(e.Text); len(sub) == 2 {
			movieID = sub[1]
		}
		if sub := regexp.MustCompile(`siteID\s*=\s*'(\d+?)';`).FindStringSubmatch(e.Text); len(sub) == 2 {
			siteID = sub[1]
		}
		if movieID == "" || siteID == "" {
			return
		}
		if sub := regexp.MustCompile(`stream\s*=\s*'(.+?)'\+siteID\+'(.+?)'\+movieId\+'(.+?)';`).
			FindStringSubmatch(e.Text); len(sub) == 4 {
			d := c.Clone()
			d.OnResponse(func(r *colly.Response) {
				defer func() {
					// Sample HLS URL
					info.PreviewVideoHLSURL = r.Request.URL.String()
				}()
				if uri, _, err := m3u8.ParseMediaURI(bytes.NewReader(r.Body)); err == nil {
					if ss := regexp.MustCompile(`/sample/(\d+)/(\d+)/ts\.(.+?)\.m3u8`).
						FindStringSubmatch(uri); len(ss) == 4 {
						info.PreviewVideoURL = fmt.Sprintf(sampleURL, ss[1], ss[2], ss[3])
					}
				}
			})
			m3u8Link := e.Request.AbsoluteURL(fmt.Sprintf("%s%s%s%s%s", sub[1], siteID, sub[2], movieID, sub[3]))
			d.Visit(m3u8Link)
		}
	})

	// Preview Images
	c.OnXML(`//div[@class="sample-images yoxview"]/script`, func(e *colly.XMLElement) {
		for _, sub := range regexp.MustCompile(`"(/contents/.+/\d+?\.\w+?)"`).FindAllStringSubmatch(e.Text, -1) {
			info.PreviewImages = append(info.PreviewImages, e.Request.AbsoluteURL(sub[1]))
		}
	})

	err = c.Visit(info.Homepage)
	return
}

func init() {
	provider.RegisterMovieFactory(Name, New)
}
