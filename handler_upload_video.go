package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	const maxMemory = 1 << 30
	r.Body = http.MaxBytesReader(w, r.Body, maxMemory)

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	fmt.Println("uploading video", "by user", userID)

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "There was an error getting video information", err)
		return
	} else if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Not Authorized", errors.New("User is not the owner of the video"))
		return
	}

	multipartFile, multipartFileHeaderPointer, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "There was an error parsing the video", err)
		return
	}
	defer multipartFile.Close()

	getContentType := multipartFileHeaderPointer.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(getContentType)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Bad Content-Type", err)
		return
	} else if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Bad Request", errors.New("Content-Type is not mp4"))
		return
	}

	tempFile, err := os.CreateTemp("", "tubley-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Something went wrong", err)
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	if _, err := io.Copy(tempFile, multipartFile); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Something went wrong", err)
		return
	}

	if _, err := tempFile.Seek(0, io.SeekStart); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Something went wrong", err)
		return
	}

	aspectRatio, err := getVideoAspectRatio(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Something went wrong", err)
		return
	}

	prefix := ""

	if aspectRatio == "16:9" {
		prefix = "landscape"
	} else if aspectRatio == "9:16" {
		prefix = "portrait"
	} else {
		prefix = aspectRatio
	}

	videoTypeSplit := strings.Split(mediaType, "/")
	if len(videoTypeSplit) != 2 {
		respondWithError(w, http.StatusInternalServerError, "Something went wrong", errors.New("Couldn't split media type"))
		return
	}

	filename := make([]byte, 32)
	rand.Read(filename)
	encodedFileName := hex.EncodeToString(filename)
	fileNameAndExtention := prefix + "/" + encodedFileName + "." + videoTypeSplit[1]

	s3params := s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &fileNameAndExtention,
		Body:        tempFile,
		ContentType: &mediaType,
	}

	if _, err := cfg.s3Client.PutObject(r.Context(), &s3params); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Something went wrong", err)
		return
	}

	s3URL := fmt.Sprintf("https://%v.s3.%v.amazonaws.com/%v", cfg.s3Bucket, cfg.s3Region, fileNameAndExtention)

	video.VideoURL = &s3URL

	if err := cfg.db.UpdateVideo(video); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Something went wrong", err)
		return
	}

}

func getVideoAspectRatio(filePath string) (string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)

	var b bytes.Buffer
	cmd.Stdout = &b

	if err := cmd.Run(); err != nil {
		return "", err
	}

	type Ratio struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	}

	type Streams struct {
		Streams []Ratio `json:"streams"`
	}

	var ratioData Streams
	if err := json.Unmarshal(b.Bytes(), &ratioData); err != nil {
		return "", err
	}

	if len(ratioData.Streams) == 0 {
		return "", errors.New("There is no data to get aspect ratio from")
	}

	width := ratioData.Streams[0].Width
	height := ratioData.Streams[0].Height

	aspectRatio := (width * 100) / height
	if aspectRatio >= 176 && aspectRatio <= 178 {
		return "16:9", nil
	} else if aspectRatio >= 55 && aspectRatio <= 57 {
		return "9:16", nil
	} else {
		return "other", nil
	}

}
