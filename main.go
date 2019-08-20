package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/lucsky/cuid"
	"github.com/marksalpeter/token"
	"image"
	"image/png"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)
	// freehand-api.local.invisionapp.com
const (
	imgPath      = "./image_list"
	urlv6        = "http://freehand-api.local.invisionapp.com/api/documents/create-from-artboard"
	urlv6convert = "http://freehand-api.local.invisionapp.com/api/documents/info"
	jwtToken     = "9CQl1FPPwyASuEbbeLYEJhmNt7kgwK8T"
	adminToken   = "abc"

)
//https://freehand-api.local.invision.works:8843

func main() {
	app := newApp()
	app.createDocumentList(1)
	log.Infof("[%s]", strings.Join(app.documentSlugs, ","))
	log.Infof("[%s]", strings.Join(app.documentIDs, ","))
}

type app struct {
	client        *http.Client
	images        []image.Image
	documentSlugs []string
	documentIDs   []string
}

type artboardScreen struct {
	ID      string  `json:"id"`
	ImageID *string `json:"imageID,omitempty"`
	Name    string  `json:"name"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Width   float64 `json:"width"`
	Height  float64 `json:"height"`
}

type ArtboardData struct {
	ID      *token.Token     `json:"id,omitempty"`
	OffsetX float64          `json:"offsetX"`
	OffsetY float64          `json:"offsetY"`
	Title   string           `json:"title"`
	Screens []artboardScreen `json:"screens"`
	Origin  *string          `json:"origin"`
}

type Response struct {
	DocumentID string `json:"id"`
	Images     map[string]struct {
		URL     string `json:"url"`
		ID      string `json:"id"`
		Created bool   `json:"created"`
	} `json:"images"`
	Url       string `json:"url"`
	Path      string `json:"path"`
	Subdomain string `json:"document_subdomain"`
}

type ConvertionResponse map[string]struct {
	ID                int    `json:"id"`
	Name              string `db:"name"`
	Team              string `db:"team"`
	Thumbnail         string `db:"thumbnail"`
	Url               string `db:"url"`
	Path              string `db:"path"`
	DocumentSubdomain string `db:"document_subdomain"`
}

func newApp() app {
	images := readFilesAndImages()
	return app{
		client: &http.Client{
			Timeout: time.Second * 30,
		},
		images: images,
	}
}

func readFilesAndImages() []image.Image {
	files, err := ioutil.ReadDir(imgPath)
	if err != nil {
		log.Fatal(err)
	}
	imageList := make([]image.Image, 0, 20)

	for _, f := range files {
		file, err := os.Open(fmt.Sprintf("%s/%s", imgPath, f.Name()))
		if err != nil {
			log.Fatal("Can't read file", err.Error())
		}
		defer func() {
			file.Close()
		}()

		imagePng, err := png.Decode(file)
		if err != nil {
			log.Fatal()
		}
		imageList = append(imageList, imagePng)
		fmt.Println(f.Name())
	}
	return imageList
}

func (a *app) createNewDocument(index int) *ArtboardData {

	offsetX := float64(rand.Intn(256))
	offsetY := float64(rand.Intn(256))
	images := make([]artboardScreen, 1)

	for i := range images {
		imageID := CreateUniqueImageID()
		randNumb := rand.Intn(10)
		image := a.images[randNumb]
		img := artboardScreen{
			ID:      fmt.Sprintf("screen_id_%d#test_%d.png", i, randNumb),
			ImageID: &imageID,
			Name:    fmt.Sprintf("screen_name_%d", i),
			X:       offsetX,
			Y:       offsetY,
			Width:   float64(image.Bounds().Bounds().Max.X),
			Height:  float64(image.Bounds().Bounds().Max.Y),
		}
		images[i] = img
	}
	return &ArtboardData{
		OffsetX: offsetX,
		OffsetY: offsetY,
		Title:   fmt.Sprintf("test_document_title_%v", index),
		Screens: images,
	}
}

func (a *app) createDocumentList(amount int) []*ArtboardData {
	list := make([]*ArtboardData, amount)
	for i := range list {
		doc := a.createNewDocument(i)
		body, err := json.Marshal(doc)
		if err != nil {
			log.Fatal("can't marshal body", err.Error())
		}
		log.Info(string(body))
		resp := a.makeRequestToV6(urlv6, bytes.NewBuffer(body))
		log.Infof("Amount of returned images: %d", len(resp.Images))
		j:=0
		for name, image := range resp.Images {
			j++
			log.Infof("fileIndex: %s", strings.Split(name, "#")[1])
			if j == 3 || j ==  2 {
				continue
			} else {

				a.makeRequestToS3D(image.URL, strings.Split(name, "#")[1])
				log.Info("pushed", j)
			}
		}
		a.convertSlugToID(resp.DocumentID, urlv6convert)
		log.Infof("slug: %s", resp.DocumentID)
	}
	return list
}

func (a *app) convertSlugToID(slug string, url string) {
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatal("can't create GET request", err.Error())
	}

	q := request.URL.Query()
	q.Add("id", slug)

	request.Header.Add("X-Admin-Token", adminToken)
	request.URL.RawQuery = q.Encode()

	resp, err := a.client.Do(request)
	if err != nil {
		log.Fatal("can't execute request", err.Error())
	}
	defer resp.Body.Close()
	responseV6Convert := ConvertionResponse{}

	bodyV6, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal("can't read body of v6 response", err.Error())
	}
	log.Warn("response from v6 api",string(bodyV6 ))
	err = json.Unmarshal(bodyV6, &responseV6Convert)
	if err != nil {
		log.Fatal("can't unmarshal v6 response", err.Error())
	}
	a.documentSlugs = append(a.documentSlugs, "\"" +slug+"\"")
	a.documentIDs = append(a.documentIDs, strconv.Itoa(responseV6Convert["document"].ID))
}

func (a *app) makeRequestToV6(url string, body io.Reader) *Response {
	request, err := http.NewRequest("POST", url, body)
	request.Header.Add("X-Access-Token", jwtToken)
	if err != nil {
		log.Fatal("can't create POST request", err.Error())
	}
	resp, err := a.client.Do(request)
	if err != nil {
		log.Fatal("can't execute request", err.Error())
	}
	defer resp.Body.Close()
	responseV6 := Response{}
	bodyV6, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal("can't read body of v6 response", err.Error())
	}
	log.Warn("response", string(bodyV6))
	err = json.Unmarshal(bodyV6, &responseV6)
	if err != nil {
		log.Fatal("can't unmarshal v6 response", err.Error())
	}
	return &responseV6
}

func (a *app) makeRequestToS3(url string, body io.Reader) {
	log.Info(url)
	request, err := http.NewRequest("PUT", url, body)
	request.Header.Add("Content-Type", "application/octet-stream")
	if err != nil {
		log.Fatal("can't create PUT request", err.Error())
	}
	resp, err := a.client.Do(request)
	if err != nil {
		log.Fatal("can't execute request", err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Warn("invalid status code on PUT request:", resp.StatusCode)
	}
}

func (a *app) makeRequestToS3D(url, fileName string) {
	fileName = fmt.Sprintf("%s/%s", imgPath, fileName)
	log.Info(url)
	file, err := ioutil.ReadFile(fileName)

	request, err := http.NewRequest("PUT", url, bytes.NewBuffer(file))
	if err != nil {
		log.Fatal("can't create PUT request", err.Error())
	}
	resp, err := a.client.Do(request)
	if err != nil {
		log.Fatal("can't execute request", err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Warn("invalid status code on PUT request:", resp.StatusCode, fileName)
	}
}

func CreateUniqueImageID() string {
	return cuid.New() + ".png"
}
