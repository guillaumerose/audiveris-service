package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/sirupsen/logrus"
)

const descriptor = "details.json"

type ProcessingStatus string

const (
	Pending    ProcessingStatus = "pending"
	InProgress ProcessingStatus = "in-progress"
	Done       ProcessingStatus = "done"
	Fail       ProcessingStatus = "fail"
)

type Score struct {
	ID          string    `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	ContentType string    `json:"content_type"`

	Status ProcessingStatus `json:"status"`
}

func upload(c echo.Context) error {
	file, err := c.FormFile("file")
	if err != nil {
		return err
	}

	contentType := file.Header.Get("Content-Type")
	if contentType != "image/png" && contentType != "image/jpeg" {
		return c.String(http.StatusBadRequest, fmt.Sprintf("bad image type: %s", contentType))
	}
	if file.Size > 10_000_000 {
		return c.String(http.StatusBadRequest, "file too big")
	}

	tmpDir, err := ioutil.TempDir(workingDir, "incoming")
	if err != nil {
		return err
	}
	hash, err := createFile(tmpDir, file, contentType)
	if err != nil {
		return err
	}
	dst := filepath.Join(workingDir, hash)
	if _, err := os.Stat(dst); os.IsNotExist(err) {
		bin, err := json.Marshal(&Score{
			ID:          hash,
			CreatedAt:   time.Now(),
			ContentType: contentType,
			Status:      Pending,
		})
		if err != nil {
			return err
		}
		if err := ioutil.WriteFile(filepath.Join(tmpDir, descriptor), bin, 0644); err != nil {
			return err
		}
		if err := os.Rename(tmpDir, dst); err != nil {
			return err
		}
		go func() {
			if err := updateStatus(hash, InProgress); err != nil {
				logrus.Error(err)
				if err := updateStatus(hash, Fail); err != nil {
					logrus.Error(err)
				}
				return
			}
			if err := convert(dst, fmt.Sprintf("input%s", ext(contentType))); err != nil {
				logrus.Error(err)
				_ = ioutil.WriteFile(filepath.Join(dst, "error.log"), []byte(err.Error()), 0644)
				if err := updateStatus(hash, Fail); err != nil {
					logrus.Error(err)
				}
				return
			}
			if err := updateStatus(hash, Done); err != nil {
				logrus.Error(err)
			}
		}()
	} else {
		_ = os.RemoveAll(tmpDir)
	}

	return c.Redirect(301, fmt.Sprintf("/sheet/%s", hash))
}

func updateStatus(id string, target ProcessingStatus) error {
	bin, err := ioutil.ReadFile(filepath.Join(workingDir, id, descriptor))
	if err != nil {
		return err
	}
	var score Score
	if err := json.Unmarshal(bin, &score); err != nil {
		return err
	}

	score.Status = target

	bin, err = json.Marshal(&score)
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(workingDir, id, descriptor), bin, 0644); err != nil {
		return err
	}
	return nil
}

func ext(contentType string) string {
	if contentType == "image/png" {
		return ".png"
	}
	if contentType == "image/jpeg" {
		return ".jpg"
	}
	return ""
}

func createFile(tmpDir string, file *multipart.FileHeader, contentType string) (string, error) {
	src, err := file.Open()
	if err != nil {
		return "", err
	}
	defer src.Close()
	dst, err := os.Create(filepath.Join(tmpDir, fmt.Sprintf("input%s", ext(contentType))))
	if err != nil {
		return "", err
	}
	defer dst.Close()
	h := sha256.New()
	if _, err = io.Copy(io.MultiWriter(dst, h), src); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func getSheet(c echo.Context) error {
	bin, err := ioutil.ReadFile(filepath.Join(workingDir, c.Param("id"), descriptor))
	if err != nil {
		return c.HTML(404, "sheet not found")
	}
	var score Score
	if err := json.Unmarshal(bin, &score); err != nil {
		return err
	}
	return c.Render(200, "sheet", score)
}

func getSheetXml(c echo.Context) error {
	bin, err := ioutil.ReadFile(filepath.Join(workingDir, c.Param("id"), descriptor))
	if err != nil {
		return err
	}
	var score Score
	if err := json.Unmarshal(bin, &score); err != nil {
		return err
	}
	switch score.Status {
	case Done:
		return c.File(filepath.Join(workingDir, score.ID, "output.xml"))
	case Fail:
		return c.String(http.StatusBadRequest, "conversion failed")
	case InProgress:
		return c.String(http.StatusNotFound, "conversion in progress")
	default:
		return c.String(http.StatusNotFound, "conversion in progress")
	}
}

func getInput(c echo.Context) error {
	bin, err := ioutil.ReadFile(filepath.Join(workingDir, c.Param("id"), descriptor))
	if err != nil {
		return err
	}
	var score Score
	if err := json.Unmarshal(bin, &score); err != nil {
		return err
	}
	return c.File(filepath.Join(workingDir, score.ID, fmt.Sprintf("input%s", ext(score.ContentType))))
}

func downloadSheet(c echo.Context) error {
	bin, err := ioutil.ReadFile(filepath.Join(workingDir, c.Param("id"), descriptor))
	if err != nil {
		return err
	}
	var score Score
	if err := json.Unmarshal(bin, &score); err != nil {
		return err
	}
	if score.Status == Done {
		return c.Attachment(filepath.Join(workingDir, score.ID, "output.mxl"), "score.mxl")
	}
	return c.String(http.StatusInternalServerError, "score not ready or conversion failed")
}

var workingDir string

func main() {
	flag.StringVar(&workingDir, "data", "./data", "Incoming sheets")
	flag.Parse()

	if err := os.MkdirAll(workingDir, 0755); err != nil {
		logrus.Fatal(err)
	}

	var err error
	workingDir, err = filepath.Abs(workingDir)
	if err != nil {
		logrus.Fatal(err)
	}

	e := echo.New()
	e.Renderer = &Template{}

	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	e.GET("/", func(c echo.Context) error {
		return c.Render(http.StatusOK, "index", nil)
	})
	e.POST("/upload", upload)

	e.GET("/sheet/:id", getSheet)
	e.GET("/sheet/:id/data", getSheetXml)
	e.GET("/sheet/:id/download", downloadSheet)
	e.GET("/sheet/:id/input", getInput)
	e.File("/public/example.jpg", "public/example.jpg")

	e.Logger.Fatal(e.Start(":1323"))
}

type Template struct {
}

func (t *Template) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	header, err := ioutil.ReadFile(filepath.Join("public", "_header.html"))
	middle, err := ioutil.ReadFile(filepath.Join("public", "views", fmt.Sprintf("%s.html", name)))
	footer, err := ioutil.ReadFile(filepath.Join("public", "_footer.html"))

	tpl, err := template.New("tpl").Parse(string(header) + string(middle) + string(footer))
	if err != nil {
		return err
	}
	return tpl.Execute(w, data)
}
