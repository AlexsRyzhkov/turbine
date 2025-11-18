package turbine

import "context"

type Context struct {
	context context.Context
	message string
}

func (c *Context) Message() string {
	return c.message
}

func (c *Context) Context() context.Context {
	return c.context
}

func (c *Context) WithContext(context context.Context) {
	c.context = context
}
