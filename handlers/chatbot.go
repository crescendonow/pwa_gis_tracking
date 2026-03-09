package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

// chatbotServiceURL is the Python text-to-query microservice URL.
var chatbotServiceURL string

func init() {
	url := os.Getenv("CHATBOT_SERVICE_URL")
	if url == "" {
		url = "http://127.0.0.1:5022"
	}
	chatbotServiceURL = url
}

// chatbotRequest is the incoming request from the frontend.
type chatbotRequest struct {
	Prompt  string `json:"prompt" binding:"required"`
	PwaCode string `json:"pwa_code"`
}

// chatbotProxyPayload is what we forward to the Python service.
type chatbotProxyPayload struct {
	Prompt     string `json:"prompt"`
	PwaCode    string `json:"pwa_code"`
	UID        string `json:"uid"`
	Permission string `json:"permission"`
}

// ChatbotQuery proxies the chatbot request to the Python text-to-query service.
// POST /api/chatbot/query
func ChatbotQuery(c *gin.Context) {
	var req chatbotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": "กรุณาพิมพ์คำถามค่ะ",
		})
		return
	}

	// Enforce max prompt length
	if len([]rune(req.Prompt)) > 500 {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": "คำถามยาวเกินไปค่ะ (สูงสุด 500 ตัวอักษร)",
		})
		return
	}

	// Get session values set by AuthRequired middleware
	uid, _ := c.Get("uid")
	pwacode, _ := c.Get("pwacode")
	permission, _ := c.Get("permission")

	// Use request pwa_code or fallback to session
	pwaCode := req.PwaCode
	if pwaCode == "" {
		if pc, ok := pwacode.(string); ok {
			pwaCode = pc
		}
	}

	uidStr := ""
	if u, ok := uid.(string); ok {
		uidStr = u
	}
	permStr := ""
	if p, ok := permission.(string); ok {
		permStr = p
	}

	// Build proxy payload
	payload := chatbotProxyPayload{
		Prompt:     req.Prompt,
		PwaCode:    pwaCode,
		UID:        uidStr,
		Permission: permStr,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "ไม่สามารถสร้างคำขอได้ค่ะ",
		})
		return
	}

	// Forward to Python service
	client := &http.Client{Timeout: 600 * time.Second}
	proxyURL := fmt.Sprintf("%s/api/text-to-query", chatbotServiceURL)

	resp, err := client.Post(proxyURL, "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("[chatbot] proxy error: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{
			"status":  "error",
			"message": "ไม่สามารถเชื่อมต่อบริการ AI ได้ค่ะ กรุณาลองใหม่อีกครั้ง",
		})
		return
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[chatbot] read response error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "เกิดข้อผิดพลาดในการอ่านผลลัพธ์ค่ะ",
		})
		return
	}

	// Forward the response as-is with the same status code
	c.Data(resp.StatusCode, "application/json; charset=utf-8", respBody)
}
