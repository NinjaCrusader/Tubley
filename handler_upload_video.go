package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
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

	videoTypeSplit := strings.Split(mediaType, "/")
	if len(videoTypeSplit) != 2 {
		respondWithError(w, http.StatusInternalServerError, "Something went wrong", errors.New("Couldn't split media type"))
		return
	}

	filename := make([]byte, 32)
	rand.Read(filename)
	encodedFileName := hex.EncodeToString(filename)
	fileNameAndExtention := encodedFileName + "." + videoTypeSplit[1]

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
