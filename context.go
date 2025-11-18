package turbine

import "context"

type Context struct {
	context context.Context
	body    string
}

func (c *Context) Body() string {
	return c.body
}

func (c *Context) Context() context.Context {
	return c.context
}

func (c *Context) WithContext(context context.Context) {
	c.context = context
}
