package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
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

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	// TODO: implement the upload here

	const maxMemory = 10 << 20
	if err := r.ParseMultipartForm(maxMemory); err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't parse multipart form.", err)
		return
	}

	multipartFile, multipartHeader, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't get thumbnail file.", err)
		return
	}

	defer multipartFile.Close()

	mediaType := multipartHeader.Header.Get("Content-Type")
	if mediaType == "" {
		respondWithError(w, http.StatusBadRequest, "Bad Request", errors.New("The Content-Type was bad"))
		return
	} else if (mediaType != "image/jpeg") && (mediaType != "image/png") {
		respondWithError(w, http.StatusBadRequest, "Bad Request", errors.New("Media type was not jpeg or png"))
		return
	}

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "There was an error getting the video", err)
		return
	}

	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Not Authorized", errors.New("User is not owner of video"))
		return
	}

	//updating handler to store image file within the assets directory

	imageTypeSplit := strings.Split(mediaType, "/")
	if len(imageTypeSplit) != 2 {
		respondWithError(w, http.StatusBadRequest, "Something went wrong", errors.New("The content type is invalid"))
		return
	}

	filenameAndExtention := videoID.String() + "." + imageTypeSplit[1]
	newPath := filepath.Join(cfg.assetsRoot, filenameAndExtention)
	newFile, err := os.Create(newPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Something went wrong", err)
		return
	}

	defer newFile.Close()

	if _, err := io.Copy(newFile, multipartFile); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Something went wrong", err)
		return
	}

	pathURL := fmt.Sprintf("http://localhost:%v/assets/%v", cfg.port, filenameAndExtention)
	video.ThumbnailURL = &pathURL

	if err := cfg.db.UpdateVideo(video); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Something went wrong updating the video url", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}
