// main.go
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
    "github.com/joho/godotenv"
	"github.com/ClearBlockchain/sdk-go/pkg/glide"
	"github.com/ClearBlockchain/sdk-go/pkg/types"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type SessionData struct {
	PhoneNumber string `json:"phoneNumber,omitempty"`
	AuthURL     string `json:"authUrl,omitempty"`
	Code        string `json:"code,omitempty"`
	Error       string `json:"error,omitempty"`
}

func main() {
    // Load environment variables from .env file if it exists
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found or error reading .env file")
	}
	// Set up the port
	port := os.Getenv("PORT")
	if port == "" {
		port = "4568"
	}

	// Initialize the GlideClient
	settings := SetupGlideSettings()
	glideClient, err := glide.NewGlideClient(settings)
	if err != nil {
		log.Fatalf("Failed to create GlideClient: %v", err)
	}

	// Initialize session data storage
	sessionData := make(map[string]*SessionData)
	var sessionDataMutex sync.Mutex
	var currentSessionData *SessionData
	var currentSessionDataMutex sync.Mutex

	// Create Echo instance
	e := echo.New()

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.BodyLimit("2M"))

	// Serve static files
	e.Static("/", "static")

	// Routes
	e.GET("/", func(c echo.Context) error {
		return c.File("static/index.html")
	})

	e.GET("/api/getAuthUrl", func(c echo.Context) error {
		phoneNumber := c.QueryParam("phoneNumber")
		if phoneNumber == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "phoneNumber is required"})
		}
		state := uuid.New().String()
		authURL, err := glideClient.NumberVerify.GetAuthURL(types.NumberVerifyAuthUrlInput{
			State:        &state,
			UseDevNumber: phoneNumber,
		})
		if err != nil {
			log.Println("Error getting auth URL:", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		// Store session data
		sessionDataMutex.Lock()
		sessionData[state] = &SessionData{
			PhoneNumber: phoneNumber,
			AuthURL:     authURL,
		}
		sessionDataMutex.Unlock()
		response := map[string]string{
			"authUrl": authURL,
		}
		log.Println("getAuthUrl res", response)
		return c.JSON(http.StatusOK, response)
	})

	e.GET("/api/getSessionData", func(c echo.Context) error {
		currentSessionDataMutex.Lock()
		defer currentSessionDataMutex.Unlock()

		if currentSessionData == nil {
			return c.JSON(http.StatusOK, map[string]interface{}{})
		}
		// Copy currentSessionData to prevent data race
		data := *currentSessionData
		// Reset session data
		sessionDataMutex.Lock()
		sessionData = make(map[string]*SessionData)
		sessionDataMutex.Unlock()
		currentSessionData = nil

		return c.JSON(http.StatusOK, data)
	})

	e.GET("/callback", func(c echo.Context) error {
		code := c.QueryParam("code")
		state := c.QueryParam("state")
		errorParam := c.QueryParam("error")
		errorDescription := c.QueryParam("error_description")
		if state == "" {
			log.Println("State parameter is missing")
			return c.Redirect(http.StatusFound, "/")
		}
		sessionDataMutex.Lock()
		session, ok := sessionData[state]
		sessionDataMutex.Unlock()
		if !ok {
			// No session data found for state
			log.Println("No session data found for state", state)
			sessionDataMutex.Lock()
			sessionData[state] = &SessionData{
				Error: "No session data found for state",
			}
			sessionDataMutex.Unlock()
			return c.Redirect(http.StatusFound, "/")
		}
		// Update session data
		sessionDataMutex.Lock()
		session.Code = code
		if errorParam != "" {
			session.Error = errorDescription
		}
		sessionDataMutex.Unlock()
		currentSessionDataMutex.Lock()
		currentSessionData = session
		currentSessionDataMutex.Unlock()
		return c.File("static/index.html")
	})

	e.POST("/api/verifyNumber", func(c echo.Context) error {
		var body struct {
			Code        string `json:"code"`
			PhoneNumber string `json:"phoneNumber"`
		}
		if err := c.Bind(&body); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request"})
		}
		if body.Code == "" || body.PhoneNumber == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "code and phoneNumber are required"})
		}
		userClient, err := glideClient.NumberVerify.For(types.NumberVerifyClientForParams{
			Code:        body.Code,
			PhoneNumber: &body.PhoneNumber,
		})
		if err != nil {
			log.Println("Error creating NumberVerifyUserClient:", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		// Get operator
		operator, err := userClient.GetOperator()
		if err != nil {
			log.Println("Error getting operator:", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		// Verify number
		verifyRes, err := userClient.VerifyNumber(nil, types.ApiConfig{SessionIdentifier: "session77"})
		if err != nil {
			log.Println("Error verifying number:", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		return c.JSON(http.StatusOK, map[string]interface{}{
			"operator":  operator,
			"verifyRes": verifyRes,
		})
	})

	// Start server
	e.Logger.Printf("Server is running on http://localhost:%s", port)
	e.Logger.Fatal(e.Start(":" + port))
}

// SetupGlideSettings initializes the Glide SDK settings
func SetupGlideSettings() types.GlideSdkSettings {
	if os.Getenv("GLIDE_CLIENT_ID") == "" {
		log.Fatal("GLIDE_CLIENT_ID environment variable is not set")
	}
	if os.Getenv("GLIDE_CLIENT_SECRET") == "" {
		log.Fatal("GLIDE_CLIENT_SECRET environment variable is not set")
	}
	if os.Getenv("GLIDE_REDIRECT_URI") == "" {
		fmt.Println("GLIDE_REDIRECT_URI environment variable is not set")
	}
	if os.Getenv("GLIDE_AUTH_BASE_URL") == "" {
		fmt.Println("GLIDE_AUTH_BASE_URL environment variable is not set")
	}
	if os.Getenv("GLIDE_API_BASE_URL") == "" {
		fmt.Println("GLIDE_API_BASE_URL environment variable is not set")
	}
	if os.Getenv("REPORT_METRIC_URL") == "" {
		fmt.Println("REPORT_METRIC_URL environment variable is not set")
	}
	return types.GlideSdkSettings{
		ClientID:     os.Getenv("GLIDE_CLIENT_ID"),
		ClientSecret: os.Getenv("GLIDE_CLIENT_SECRET"),
		RedirectURI:  os.Getenv("GLIDE_REDIRECT_URI"),
		Internal: types.InternalSettings{
			AuthBaseURL: os.Getenv("GLIDE_AUTH_BASE_URL"),
			APIBaseURL:  os.Getenv("GLIDE_API_BASE_URL"),
		},
	}
}
