package api

func (c *Ctx) orgGoogleStatus() error {
	if _, err := c.requireLeadership(); err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"configured": c.S.cfg.Google != nil, "connected": false, "email": nil})
	return nil
}
func (c *Ctx) orgGoogleDisconnect() error { return c.orgGoogleStatus() }
