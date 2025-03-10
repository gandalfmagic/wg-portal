package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	wgportal "github.com/h44z/wg-portal"
	_ "github.com/h44z/wg-portal/internal/server/docs" // docs is generated by Swag CLI, you have to import it.
	ginSwagger "github.com/swaggo/gin-swagger"
	"github.com/swaggo/gin-swagger/swaggerFiles"
	csrf "github.com/utrack/gin-csrf"
)

func SetupRoutes(s *Server) {
	csrfMiddleware := csrf.Middleware(csrf.Options{
		Secret: s.config.Core.SessionSecret,
		ErrorFunc: func(c *gin.Context) {
			c.String(400, "CSRF token mismatch")
			c.Abort()
		},
	})

	// Startpage
	s.server.GET("/", s.GetIndex)
	s.server.GET("/favicon.ico", func(c *gin.Context) {
		file, _ := wgportal.Statics.ReadFile("assets/img/favicon.ico")
		c.Data(
			http.StatusOK,
			"image/x-icon",
			file,
		)
	})

	// Auth routes
	auth := s.server.Group("/auth")
	auth.Use(csrfMiddleware)
	auth.GET("/login", s.GetLogin)
	auth.POST("/login", s.PostLogin)
	auth.GET("/logout", s.GetLogout)

	if s.config.OAUTH.IsEnabled() || s.config.OIDC.IsEnabled() {
		oauth := s.server.Group("/oauth")
		oauth.Use(csrfMiddleware)
		oauth.POST("/login", s.OAuthLogin)
		oauth.GET(s.config.OAUTH.RedirectURL, s.OAuthCallback)
	}

	// Admin routes
	admin := s.server.Group("/admin")
	admin.Use(csrfMiddleware)
	admin.Use(s.RequireAuthentication("admin"))
	admin.GET("/", s.GetAdminIndex)
	admin.GET("/device/edit", s.GetAdminEditInterface)
	admin.POST("/device/edit", s.PostAdminEditInterface)
	admin.GET("/device/download", s.GetInterfaceConfig)
	admin.GET("/device/write", s.GetSaveConfig)
	admin.GET("/device/applyglobals", s.GetApplyGlobalConfig)
	admin.GET("/peer/edit", s.GetAdminEditPeer)
	admin.POST("/peer/edit", s.PostAdminEditPeer)
	admin.GET("/peer/create", s.GetAdminCreatePeer)
	admin.POST("/peer/create", s.PostAdminCreatePeer)
	admin.GET("/peer/createldap", s.GetAdminCreateLdapPeers)
	admin.POST("/peer/createldap", s.PostAdminCreateLdapPeers)
	admin.GET("/peer/delete", s.GetAdminDeletePeer)
	admin.GET("/peer/download", s.GetPeerConfig)
	admin.GET("/peer/email", s.GetPeerConfigMail)
	admin.GET("/peer/emailall", s.GetAdminSendEmails)

	admin.GET("/users/", s.GetAdminUsersIndex)
	admin.GET("/users/create", s.GetAdminUsersCreate)
	admin.POST("/users/create", s.PostAdminUsersCreate)
	admin.GET("/users/edit", s.GetAdminUsersEdit)
	admin.GET("/users/delete", s.GetAdminUsersDelete)
	admin.POST("/users/edit", s.PostAdminUsersEdit)

	// User routes
	user := s.server.Group("/user")
	user.Use(csrfMiddleware)
	user.Use(s.RequireAuthentication("")) // empty scope = all logged in users
	user.GET("/qrcode", s.GetPeerQRCode)
	user.GET("/profile", s.GetUserIndex)
	user.GET("/download", s.GetPeerConfig)
	user.GET("/email", s.GetPeerConfigMail)
	user.GET("/status", s.GetPeerStatus)

	if s.config.WG.UserManagePeers {
		user.GET("/peer/create", s.GetUserCreatePeer)
		user.POST("/peer/create", s.PostUserCreatePeer)
		user.GET("/peer/edit", s.GetUserEditPeer)
		user.POST("/peer/edit", s.PostUserEditPeer)
	}
}

func SetupApiRoutes(s *Server) {
	api := ApiServer{s: s}

	// Admin authenticated routes
	apiV1Backend := s.server.Group("/api/v1/backend")
	apiV1Backend.Use(s.RequireApiAuthentication("admin"))

	apiV1Backend.GET("/users", api.GetUsers)
	apiV1Backend.POST("/users", api.PostUser)
	apiV1Backend.GET("/user", api.GetUser)
	apiV1Backend.PUT("/user", api.PutUser)
	apiV1Backend.PATCH("/user", api.PatchUser)
	apiV1Backend.DELETE("/user", api.DeleteUser)

	apiV1Backend.GET("/peers", api.GetPeers)
	apiV1Backend.POST("/peers", api.PostPeer)
	apiV1Backend.GET("/peer", api.GetPeer)
	apiV1Backend.PUT("/peer", api.PutPeer)
	apiV1Backend.PATCH("/peer", api.PatchPeer)
	apiV1Backend.DELETE("/peer", api.DeletePeer)

	apiV1Backend.GET("/devices", api.GetDevices)
	apiV1Backend.GET("/device", api.GetDevice)
	apiV1Backend.PUT("/device", api.PutDevice)
	apiV1Backend.PATCH("/device", api.PatchDevice)

	// Simple authenticated routes
	apiV1Deployment := s.server.Group("/api/v1/provisioning")
	apiV1Deployment.Use(s.RequireApiAuthentication(""))

	apiV1Deployment.GET("/peers", api.GetPeerDeploymentInformation)
	apiV1Deployment.GET("/peer", api.GetPeerDeploymentConfig)
	apiV1Deployment.POST("/peers", api.PostPeerDeploymentConfig)

	// Swagger doc/ui
	s.server.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
}

func (s *Server) RequireAuthentication(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := GetSessionData(c)

		if !session.LoggedIn {
			// Abort the request with the appropriate error code
			c.Abort()
			c.Redirect(http.StatusSeeOther, "/auth/login?err=loginreq")
			return
		}

		if scope == "admin" && !session.IsAdmin {
			// Abort the request with the appropriate error code
			c.Abort()
			s.GetHandleError(c, http.StatusUnauthorized, "unauthorized", "not enough permissions")
			return
		}

		// default case if some random scope was set...
		if scope != "" && !session.IsAdmin {
			// Abort the request with the appropriate error code
			c.Abort()
			s.GetHandleError(c, http.StatusUnauthorized, "unauthorized", "not enough permissions")
			return
		}

		// Check if logged-in user is still valid
		if !s.isUserStillValid(session.Email) {
			_ = DestroySessionData(c)
			c.Abort()
			s.GetHandleError(c, http.StatusUnauthorized, "unauthorized", "session no longer available")
			return
		}

		// Continue down the chain to handler etc
		c.Next()
	}
}

func (s *Server) RequireApiAuthentication(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		username, password, hasAuth := c.Request.BasicAuth()
		if !hasAuth {
			c.Abort()
			c.JSON(http.StatusUnauthorized, ApiError{Message: "unauthorized"})
			return
		}

		// Validate form input
		if strings.Trim(username, " ") == "" || strings.Trim(password, " ") == "" {
			c.Abort()
			c.JSON(http.StatusUnauthorized, ApiError{Message: "unauthorized"})
			return
		}

		// Check all available auth backends
		user, err := s.checkAuthentication(username, password)
		if err != nil {
			c.Abort()
			c.JSON(http.StatusInternalServerError, ApiError{Message: "login error"})
			return
		}

		// Check if user is authenticated
		if user == nil {
			c.Abort()
			c.JSON(http.StatusUnauthorized, ApiError{Message: "unauthorized"})
			return
		}

		// Check admin scope
		if scope == "admin" && !user.IsAdmin {
			// Abort the request with the appropriate error code
			c.Abort()
			c.JSON(http.StatusForbidden, ApiError{Message: "unauthorized"})
			return
		}

		// default case if some random scope was set...
		if scope != "" && !user.IsAdmin {
			// Abort the request with the appropriate error code
			c.Abort()
			c.JSON(http.StatusForbidden, ApiError{Message: "unauthorized"})
			return
		}

		// Continue down the chain to handler etc
		c.Next()
	}
}
