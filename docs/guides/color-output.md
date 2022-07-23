---
title: "Color output controls"
description: "Modifying colors of progress output"
keywords: build, buildx, buildkit
---

Buildx has support for modifying the colors that are used to output information
to the terminal. You can set the environment variable `BUILDKIT_COLORS` to
something like `run=123,20,245:error=yellow:cancel=blue:warning=white` to set
the colors that you would like to use:

![Progress output custom colors](https://user-images.githubusercontent.com/1951866/180584033-24522385-cafd-4a54-a4a2-18f5ce74eb27.png)

Setting `NO_COLOR` to anything will disable any colorized output as recommended
by [no-color.org](https://no-color.org/):

![Progress output no color](https://user-images.githubusercontent.com/1951866/180584037-e28f9997-dd4c-49cf-8b26-04864815de19.png)

> **Note**
>
> Parsing errors will be reported but ignored. This will result in default
> color values being used where needed.

See also [the list of pre-defined colors](https://github.com/moby/buildkit/blob/master/util/progress/progressui/colors.go).
