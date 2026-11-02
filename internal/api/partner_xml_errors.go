package api

import (
	"bytes"
	"net/http"

	"github.com/gin-gonic/gin"

	"simlive/internal/partner"
	"simlive/internal/utils"
)

// xmlContentType is the response content type for every /common response,
// including error documents (so partners always get parseable XML).
const xmlContentType = "application/xml; charset=utf-8"

// writeXML encodes root as an XML document with the given HTTP status.
func writeXML(c *gin.Context, status int, root *partner.Node) {
	var buf bytes.Buffer
	if err := partner.Encode(&buf, root); err != nil {
		c.String(http.StatusInternalServerError, "failed to encode XML")
		return
	}
	c.Data(status, xmlContentType, buf.Bytes())
}

// writeXMLError emits a well-formed <error> document with the given status.
func writeXMLError(c *gin.Context, status int, code, message string) {
	writeXML(c, status, partner.ErrorDocument(code, message))
}

// logPartnerError reports a configuration or upstream problem to operators
// without leaking upstream payloads to the partner response.
func logPartnerError(scope string, err error) {
	if err == nil {
		return
	}
	utils.GetLogger("PARTNER").Error(scope, "error", err.Error())
}
