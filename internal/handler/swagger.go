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
    "description": "Local SQLite backend for users, language settings and notes."
  },
  "paths": {
    "/api/v1/users/register": {
      "post": {
        "summary": "Register user",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/RegisterRequest" }
            }
          }
        },
        "responses": {
          "201": {
            "description": "User created",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/User" }
              }
            }
          }
        }
      }
    },
    "/api/v1/users/login": {
      "post": {
        "summary": "Login by email/password",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": { "$ref": "#/components/schemas/LoginRequest" }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Authenticated user with tokens",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/LoginResponse" }
              }
            }
          }
        }
      }
    },
    "/api/v1/users/{id}": {
      "get": {
        "summary": "Get user by id",
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "required": true,
            "schema": { "type": "string", "format": "uuid" }
          }
        ],
        "responses": {
          "200": {
            "description": "User info",
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
          {
            "name": "id",
            "in": "path",
            "required": true,
            "schema": { "type": "string", "format": "uuid" }
          }
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
        "summary": "Create note",
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
            "description": "Created note",
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
        "summary": "List notes by user",
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "required": true,
            "schema": { "type": "string", "format": "uuid" }
          }
        ],
        "responses": {
          "200": {
            "description": "Notes list"
          }
        }
      }
    },
    "/swagger.json": {
      "get": {
        "summary": "OpenAPI spec"
      }
    }
  },
  "components": {
    "schemas": {
      "RegisterRequest": {
        "type": "object",
        "required": ["email", "password"],
        "properties": {
          "email": { "type": "string", "format": "email" },
          "password": { "type": "string" },
          "userLanguage": { "type": "string", "example": "en" }
        }
      },
      "LoginRequest": {
        "type": "object",
        "required": ["email", "password"],
        "properties": {
          "email": { "type": "string", "format": "email" },
          "password": { "type": "string" }
        }
      },
      "TokenPair": {
        "type": "object",
        "properties": {
          "access_token": { "type": "string" },
          "refresh_token": { "type": "string" }
        }
      },
      "LoginResponse": {
        "type": "object",
        "properties": {
          "user": { "$ref": "#/components/schemas/User" },
          "tokens": { "$ref": "#/components/schemas/TokenPair" }
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
          "userLanguage": { "type": "string" },
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
