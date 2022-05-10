package xslist

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/javtube/javtube-sdk-go/common/parser"
	"github.com/javtube/javtube-sdk-go/common/random"
	"github.com/javtube/javtube-sdk-go/model"
	"github.com/javtube/javtube-sdk-go/provider"
	"golang.org/x/net/html"
)

var (
	_ provider.ActorProvider = (*XsList)(nil)
	_ provider.ActorSearcher = (*XsList)(nil)
)

const name = "xslist"

const (
	baseURL   = "https://xslist.org/"
	actorURL  = "https://xslist.org/zh/model/%s.html"
	searchURL = "https://xslist.org/search?query=%s&lg=zh"
)

type XsList struct {
	c *colly.Collector
}

func New() *XsList {
	return &XsList{
		c: colly.NewCollector(
			colly.AllowURLRevisit(),
			colly.IgnoreRobotsTxt(),
			colly.UserAgent(random.UserAgent())),
	}
}

func (xsl *XsList) Name() string {
	return name
}

func (xsl *XsList) GetActorInfoByID(id string) (info *model.ActorInfo, err error) {
	return xsl.GetActorInfoByURL(fmt.Sprintf(actorURL, id))
}

func (xsl *XsList) GetActorInfoByURL(u string) (info *model.ActorInfo, err error) {
	homepage, err := url.Parse(u)
	if err != nil {
		return nil, err
	}
	var id string
	if ext := path.Ext(homepage.Path); ext != "" {
		id = path.Base(homepage.Path[:len(homepage.Path)-len(ext)])
	}

	info = &model.ActorInfo{
		ID:       id,
		Provider: name,
		Homepage: homepage.String(),
		Aliases:  []string{},
		Images:   []string{},
	}

	c := xsl.c.Clone()

	// Name
	c.OnXML(`//*[@id="sss1"]/header/h1/span`, func(e *colly.XMLElement) {
		info.Name = e.Text
	})

	// Aliases
	c.OnXML(`//*[@id="sss1"]/p/span`, func(e *colly.XMLElement) {
		info.Aliases = append(info.Aliases, e.Text)
	})

	// Images
	c.OnXML(`//*[@id="gallery"]/a`, func(e *colly.XMLElement) {
		if e.Attr("class") == "profile_img" {
			return // ignore profile image due to its poor resolution
		}
		width := parser.ParseInt(e.Attr("data-width"))
		height := parser.ParseInt(e.Attr("data-height"))
		if width == 0 || height == 0 {
			return // width & height
		}
		info.Images = append(info.Images, e.Attr("href"))
	})

	// Fields
	c.OnXML(`//*[@id="layout"]/div/p[1]`, func(e *colly.XMLElement) {
		for n := e.DOM.(*html.Node).FirstChild; n != nil; n = n.NextSibling {
			if n.Type != html.TextNode {
				continue
			}
			if ss := strings.SplitN(strings.TrimSpace(n.Data), ":", 2); len(ss) == 2 {
				if ss[1] = strings.TrimSpace(ss[1]); ss[1] == "" || ss[1] == "n/a" {
					continue
				}
				switch ss[0] {
				case "出生":
					info.Birthday = parser.ParseDate(ss[1])
				case "三围":
					info.Measurements = strings.ReplaceAll(ss[1], " ", "")
				case "罩杯":
					info.CupSize = strings.TrimSpace(strings.TrimSuffix(ss[1], "Cup"))
				case "出道日期":
					info.DebutDate = parseDebutDate(ss[1])
				case "血型":
					info.BloodType = ss[1]
				case "身高":
					info.Height = parser.ParseInt(strings.TrimRight(ss[1], "cm"))
				case "国籍":
					info.Nationality = ss[1]
				}
			}
		}
	})

	// Height
	c.OnXML(`//span[@itemprop="height"]`, func(e *colly.XMLElement) {
		info.Height = parser.ParseInt(strings.TrimRight(e.Text, "cm")) // ignore n/a
	})

	// Nationality
	c.OnXML(`//span[@itemprop="nationality"]`, func(e *colly.XMLElement) {
		info.Nationality = strings.ReplaceAll(e.Text, "n/a", "")
	})

	err = c.Visit(info.Homepage)
	return
}

func (xsl *XsList) SearchActor(keyword string) (results []*model.ActorSearchResult, err error) {
	c := xsl.c.Clone()

	c.OnXML(`//ul/li`, func(e *colly.XMLElement) {
		homepage, _ := url.Parse(e.ChildAttr(`.//h3/a`, "href"))
		id := path.Base(homepage.Path)
		if ext := path.Ext(id); ext != "" {
			id = id[:len(id)-len(ext)]
		}
		// Name
		name := e.ChildAttr(`.//h3/a`, "title")
		if ss := strings.SplitN(name, "-", 2); len(ss) == 2 {
			name = strings.TrimSpace(ss[1])
		}
		// Images
		var images []string
		if img := e.ChildAttr(`.//div[1]/img`, "src"); img != "" {
			images = []string{e.Request.AbsoluteURL(img)}
		}
		results = append(results, &model.ActorSearchResult{
			ID:       id,
			Name:     name,
			Images:   images,
			Provider: name,
			Homepage: homepage.String(),
		})
	})

	err = c.Visit(fmt.Sprintf(searchURL, url.QueryEscape(keyword)))
	return
}

func parseDebutDate(s string) time.Time {
	if ss := regexp.MustCompile(`^([\s\d]+)年([\s\d]+)月$`).
		FindStringSubmatch(s); len(ss) == 3 {
		return time.Date(parser.ParseInt(ss[1]), time.Month(parser.ParseInt(ss[2])),
			1, 0, 0, 0, 0, time.UTC)
	}
	return parser.ParseDate(s)
}

func init() {
	provider.RegisterActorFactory(name, New)
}