package handler

import "net/http"

const swaggerUIHTML = `<!doctype html>
<html>
<head>
  <meta charset="utf-8" />
  <title>Timeleak Swagger</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" />
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.ui = SwaggerUIBundle({ url: "/swagger.json", dom_id: "#swagger-ui" });
  </script>
</body>
</html>`

const swaggerSpec = `{
  "openapi": "3.0.3",
  "info": {
    "title": "Timeleak API",
    "version": "1.0.0",
    "description": "Versioned API under /api/v1 with WhatsApp OTP auth, refresh rotation, admin auth, and ads rotation."
  },
  "servers": [
    { "url": "/" }
  ],
  "paths": {
    "/health": {
      "get": {
        "summary": "Health check",
        "responses": {
          "200": { "description": "OK" }
        }
      }
    },
    "/api/v1/auth/otp/request": {
      "post": {
        "summary": "Request WhatsApp OTP",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/OTPRequestInput" }
            }
          }
        },
        "responses": {
          "200": {
            "description": "OTP requested",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/OTPRequestResponse" }
              }
            }
          },
          "429": { "description": "Rate limited or temporarily locked" }
        }
      }
    },
    "/api/v1/auth/otp/verify": {
      "post": {
        "summary": "Verify WhatsApp OTP and issue token pair",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/OTPVerifyInput" }
            }
          }
        },
        "responses": {
          "200": {
            "description": "OTP verified",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/OTPVerifyResponse" }
              }
            }
          },
          "400": { "description": "Invalid OTP payload or code" },
          "429": { "description": "Too many attempts" }
        }
      }
    },
    "/api/v1/auth/refresh": {
      "post": {
        "summary": "Rotate refresh token and get new token pair",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/RefreshRequest" }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Refreshed",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/TokenPayload" }
              }
            }
          },
          "401": { "description": "Invalid/expired/revoked refresh token" }
        }
      }
    },
    "/api/v1/auth/me": {
      "get": {
        "summary": "Legacy token claims endpoint",
        "security": [{ "BearerAuth": [] }],
        "responses": {
          "200": { "description": "Claims" },
          "401": { "description": "Unauthorized" }
        }
      }
    },
    "/api/v1/me": {
      "get": {
        "summary": "Get current user profile",
        "security": [{ "BearerAuth": [] }],
        "responses": {
          "200": {
            "description": "Current user",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/User" }
              }
            }
          },
          "401": { "description": "Unauthorized" }
        }
      }
    },
    "/api/v1/auth/notes": {
      "post": {
        "summary": "Create note for authenticated user",
        "security": [{ "BearerAuth": [] }],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["note_type"],
                "properties": {
                  "note_type": { "type": "string", "example": "deadline" }
                }
              }
            }
          }
        },
        "responses": {
          "201": {
            "description": "Created",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/Note" }
              }
            }
          },
          "401": { "description": "Unauthorized" }
        }
      },
      "get": {
        "summary": "List notes for authenticated user",
        "security": [{ "BearerAuth": [] }],
        "responses": {
          "200": {
            "description": "Notes",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/NotesListResponse" }
              }
            }
          },
          "401": { "description": "Unauthorized" }
        }
      }
    },
    "/api/v1/users/{id}": {
      "get": {
        "summary": "Get user by ID",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "schema": { "type": "string", "format": "uuid" } }
        ],
        "responses": {
          "200": {
            "description": "User",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/User" }
              }
            }
          }
        }
      }
    },
    "/api/v1/users/{id}/language": {
      "put": {
        "summary": "Update user language",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "schema": { "type": "string", "format": "uuid" } }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/LanguageRequest" }
            }
          }
        },
        "responses": {
          "200": { "description": "Updated" }
        }
      }
    },
    "/api/v1/notes": {
      "post": {
        "summary": "Create note by userId",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/CreateNoteRequest" }
            }
          }
        },
        "responses": {
          "201": {
            "description": "Created",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/Note" }
              }
            }
          }
        }
      }
    },
    "/api/v1/users/{id}/notes": {
      "get": {
        "summary": "List notes by user ID",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "schema": { "type": "string", "format": "uuid" } }
        ],
        "responses": {
          "200": {
            "description": "Notes",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/NotesListResponse" }
              }
            }
          }
        }
      }
    },
    "/api/v1/admin/auth/login": {
      "post": {
        "summary": "Admin login",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/AdminLoginRequest" }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Authenticated",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/AdminLoginResponse" }
              }
            }
          },
          "401": { "description": "Unauthorized" }
        }
      }
    },
    "/api/v1/admin/ads": {
      "post": {
        "summary": "Create ad",
        "security": [{ "BearerAuth": [] }],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/CreateAdRequest" }
            }
          }
        },
        "responses": {
          "201": {
            "description": "Created",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/Ad" }
              }
            }
          }
        }
      },
      "get": {
        "summary": "List ads",
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "limit", "in": "query", "schema": { "type": "integer", "default": 20 } },
          { "name": "offset", "in": "query", "schema": { "type": "integer", "default": 0 } },
          { "name": "active", "in": "query", "schema": { "type": "boolean" } }
        ],
        "responses": {
          "200": {
            "description": "Ads list",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/AdsListResponse" }
              }
            }
          }
        }
      }
    },
    "/api/v1/admin/ads/{id}": {
      "put": {
        "summary": "Update ad",
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/UpdateAdRequest" }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Updated",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/Ad" }
              }
            }
          }
        }
      },
      "delete": {
        "summary": "Delete ad",
        "security": [{ "BearerAuth": [] }],
        "parameters": [
          { "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }
        ],
        "responses": {
          "200": { "description": "Deleted" }
        }
      }
    },
    "/api/v1/ads/next": {
      "get": {
        "summary": "Get next ad for authenticated user",
        "security": [{ "BearerAuth": [] }],
        "responses": {
          "200": {
            "description": "Ad",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/Ad" }
              }
            }
          },
          "204": { "description": "No active ads" }
        }
      }
    },
    "/api/v1/admin/testing/otp/latest": {
      "get": {
        "summary": "Get latest OTP code (DEV ONLY)",
        "description": "DEV ONLY: returns OTP code in plain text for testing. Requires ENABLE_TESTING_ENDPOINTS=true.",
        "parameters": [
          { "name": "phone", "in": "query", "required": true, "schema": { "type": "string", "example": "+77015556677" } }
        ],
        "responses": {
          "200": {
            "description": "OTP code in plain text",
            "content": {
              "text/plain": {
                "schema": { "type": "string", "example": "1234" }
              }
            }
          },
          "404": { "description": "Disabled or code not found" }
        }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "BearerAuth": {
        "type": "http",
        "scheme": "bearer",
        "bearerFormat": "JWT"
      }
    },
    "schemas": {
      "ErrorResponse": {
        "type": "object",
        "properties": {
          "error": { "type": "string", "example": "unauthorized" }
        }
      },
      "OTPRequestInput": {
        "type": "object",
        "required": ["phone"],
        "properties": {
          "phone": { "type": "string", "example": "+77015556677" }
        }
      },
      "OTPRequestResponse": {
        "type": "object",
        "properties": {
          "request_id": { "type": "string" },
          "expires_in_seconds": { "type": "integer", "example": 60 }
        }
      },
      "OTPVerifyInput": {
        "type": "object",
        "required": ["request_id", "code"],
        "properties": {
          "request_id": { "type": "string" },
          "code": { "type": "string", "example": "1234" }
        }
      },
      "OTPVerifyResponse": {
        "type": "object",
        "properties": {
          "access_token": { "type": "string" },
          "refresh_token": { "type": "string" },
          "expires_in_seconds": { "type": "integer", "example": 60 },
          "user": { "$ref": "#/components/schemas/User" }
        }
      },
      "TokenPayload": {
        "type": "object",
        "properties": {
          "access_token": { "type": "string" },
          "refresh_token": { "type": "string" },
          "expires_in_seconds": { "type": "integer", "example": 60 }
        }
      },
      "RefreshRequest": {
        "type": "object",
        "required": ["refresh_token"],
        "properties": {
          "refresh_token": { "type": "string" }
        }
      },
      "LanguageRequest": {
        "type": "object",
        "required": ["userLanguage"],
        "properties": {
          "userLanguage": { "type": "string", "example": "ru" }
        }
      },
      "CreateNoteRequest": {
        "type": "object",
        "required": ["userId", "note_type"],
        "properties": {
          "userId": { "type": "string", "format": "uuid" },
          "note_type": { "type": "string", "example": "deadline" }
        }
      },
      "User": {
        "type": "object",
        "properties": {
          "userId": { "type": "string", "format": "uuid" },
          "email": { "type": "string", "format": "email" },
          "phone": { "type": "string", "example": "+77015556677" },
          "userLanguage": { "type": "string", "example": "en" },
          "createdAt": { "type": "string", "format": "date-time" }
        }
      },
      "Note": {
        "type": "object",
        "properties": {
          "id": { "type": "string", "format": "uuid" },
          "userId": { "type": "string", "format": "uuid" },
          "note_type": { "type": "string" },
          "createdAt": { "type": "string", "format": "date-time" }
        }
      },
      "NotesListResponse": {
        "type": "object",
        "properties": {
          "data": {
            "type": "array",
            "items": { "$ref": "#/components/schemas/Note" }
          },
          "total": { "type": "integer" }
        }
      },
      "AdminLoginRequest": {
        "type": "object",
        "required": ["username", "password"],
        "properties": {
          "username": { "type": "string", "example": "Admin" },
          "password": { "type": "string", "example": "QRT123" }
        }
      },
      "AdminLoginResponse": {
        "type": "object",
        "properties": {
          "access_token": { "type": "string" },
          "expires_in_seconds": { "type": "integer", "example": 60 }
        }
      },
      "CreateAdRequest": {
        "type": "object",
        "required": ["title", "image_url", "target_url"],
        "properties": {
          "title": { "type": "string", "example": "Try premium" },
          "image_url": { "type": "string", "example": "https://cdn.example.com/ad.png" },
          "target_url": { "type": "string", "example": "https://example.com/promo" },
          "is_active": { "type": "boolean", "example": true }
        }
      },
      "UpdateAdRequest": {
        "type": "object",
        "properties": {
          "title": { "type": "string" },
          "image_url": { "type": "string" },
          "target_url": { "type": "string" },
          "is_active": { "type": "boolean" }
        }
      },
      "Ad": {
        "type": "object",
        "properties": {
          "id": { "type": "string" },
          "title": { "type": "string" },
          "image_url": { "type": "string" },
          "target_url": { "type": "string" },
          "is_active": { "type": "boolean" },
          "created_at": { "type": "string", "format": "date-time" },
          "updated_at": { "type": "string", "format": "date-time" }
        }
      },
      "AdsListResponse": {
        "type": "object",
        "properties": {
          "data": {
            "type": "array",
            "items": { "$ref": "#/components/schemas/Ad" }
          },
          "total": { "type": "integer" }
        }
      }
    }
  }
}`

func (h *Handler) SwaggerJSON(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(swaggerSpec))
}

func (h *Handler) SwaggerUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(swaggerUIHTML))
}
