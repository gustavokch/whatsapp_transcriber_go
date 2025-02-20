package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"time"
)

type CFTranscriber struct {
	AccountID string
	APIToken  string
	Model     string
	Language  string
	BaseURL   string
}

func NewCFTranscriber(accountID, apiToken, model, language string) *CFTranscriber {
	if model == "" {
		model = "@cf/openai/whisper-large-v3-turbo"
	}
	if language == "" {
		language = "en"
	}
	baseURL := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/run/%s", accountID, model)
	return &CFTranscriber{
		AccountID: accountID,
		APIToken:  apiToken,
		Model:     model,
		Language:  language,
		BaseURL:   baseURL,
	}
}

func (cf *CFTranscriber) encodeAudioFile(filePath string) (string, error) {
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func (cf *CFTranscriber) Transcribe(audioPath string, language string) (map[string]interface{}, error) {
	encodedAudio, err := cf.encodeAudioFile(audioPath)
	if err != nil {
		return nil, err
	}
	if language == "" {
		language = "en"
	}
	payload := map[string]interface{}{
		"model":      cf.Model,
		"audio":      encodedAudio,
		"language":   language,
		"vad_filter": "false",
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", cf.BaseURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", cf.APIToken))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("transcription failed: %s", string(bodyBytes))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}

// CfTranscribe is a helper function that loads credentials from .env and transcribes the audio.
func CfTranscribe(audioPath, model, language string) (string, error) {
	accountID := os.Getenv("CF_ACCOUNT_ID")
	apiToken := os.Getenv("CF_API_KEY")
	if accountID == "" || apiToken == "" {
		return "", fmt.Errorf("please set CF_ACCOUNT_ID and CF_API_KEY environment variables")
	}

	transcriber := NewCFTranscriber(accountID, apiToken, model, language)
	result, err := transcriber.Transcribe(audioPath, language)
	if err != nil {
		return "", err
	}
	// Assuming the response JSON contains result.text
	if res, ok := result["result"].(map[string]interface{}); ok {
		if text, ok := res["text"].(string); ok {
			return text, nil
		}
	}
	return "", fmt.Errorf("transcription text not found in response")
}
