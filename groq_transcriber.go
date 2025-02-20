package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func TranscribeAudioGroq(audioPath, prompt, language string) (string, error) {
	apiKey := os.Getenv("GROQ_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("GROQ_API_KEY not set in environment")
	}

	// Read audio file
	fileData, err := os.ReadFile(audioPath)
	if err != nil {
		return "", fmt.Errorf("audio file not found: %v", err)
	}

	// Prepare multipart form data
	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	// Add file field
	fileWriter, err := w.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return "", err
	}
	if _, err = fileWriter.Write(fileData); err != nil {
		return "", err
	}

	// Add other fields
	w.WriteField("model", "whisper-large-v3") // Adjust model logic as needed
	if prompt != "" {
		w.WriteField("prompt", prompt)
	}
	if language != "" {
		w.WriteField("language", language)
	}
	w.WriteField("response_format", "json")
	w.WriteField("temperature", "0.0")

	w.Close()

	req, err := http.NewRequest("POST", "https://api.groq.com/v1/audio/transcriptions", &b)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	req.Header.Set("Content-Type", w.FormDataContentType())

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		return "", fmt.Errorf("transcription failed: %s", string(bodyBytes))
	}

	var response struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", err
	}

	return response.Text, nil
}
