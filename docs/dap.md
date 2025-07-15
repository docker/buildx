# Debug Adapter Protocol

## What is the debug adapter protocol?

The debug adapter protocol [DAP](https://microsoft.github.io/debug-adapter-protocol/overview) is a protocol created by Microsoft to standardize an abstract protocol for how a development tool (such as VS Code) communicates with concrete debuggers.

Many [popular editors](https://microsoft.github.io/debug-adapter-protocol/implementors/tools/) now support DAP as the primary method of launching debuggers. This includes **VS Code** and **Neovim**. There are also other editors, such as **Jetbrains IDEs** where DAP is not the primary method of launching debuggers, but is available with plugins.

## Features

- Pause on exception.
- Set breakpoints on instructions.
- Step next and continue.

## Limitations

- **Step In** is the same as **Next**.
- **Step Out** is the same as **Continue**.
- **FROM** directives may have unintuitive breakpoint lines.
- Stack traces may not show the full sequence of events.
- Invalid `args` in launch request may not produce an error in the UI.
- Does not support arbitrary pausing.
- Output is always the plain text printer.

## Future Improvements

- Support for Bake.
- Open terminal in an intermediate container image.
- Backwards stepping.
- Better UI for errors with invalid arguments.

## We would like feedback on

- Stack traces.
- Step/pause locations.
- Variable inspections.
- Additional information that would be helpful while debugging.

### Stack Traces

We would like feedback on whether the stack traces are easy to read and useful for debugging.

The goal was to include the parent commands inside of a stack trace to make it easier to understand the previous commands used to reach the current step. Stack traces in normal programming languages will only have one parent (the calling function).

In a Dockerfile, there are no functions which makes displaying a call stack not useful. Instead, we decided to show the input to the step as the "calling function" to make it easier to see the preceding steps.

This method of showing a stack trace is not always clear. When a step has multiple parents, such as a `COPY --from` or a `RUN` with a bind mount, there are multiple parents. Only one can be the official "parent" in the stack trace. At the moment, we do not try to choose one and will break the stack trace into two separate call stacks. This is also the case when one step is used as the parent for multiple steps.

### Step/pause Locations

Execution is paused **after** the step has been executed rather than before.

For example:

```dockerfile
FROM busybox
RUN echo hello > /hello
```

If you set a breakpoint on line 2, then execution will pause **after** the `RUN` has executed rather than before.

We thought this method would be more useful because we figured it was more common to want to inspect the state after a step rather than before the step.

There are also Dockerfiles where some instructions are aliases for another instruction and don't have their own representation in the Dockerfile.

```dockerfile
FROM golang:1.24 AS golang-base

# Does not show up as a breakpoint since it refers to the instruction
# from earlier.
FROM golang-base
RUN go build ...
```

### Step into/out

It is required to implement these for a debug adapter but we haven't determined a way that these map to Dockerfile execution. Feedback about how you would expect these to work would be helpful for future development.

For now, step into is implemented the same as next while step out is implemented the same as continue. The logic here is that next step is always going into the next call and stepping out would be returning from the current function which is the same as building the final step.

### Variable Inspections

We plan to include more variable inspections but we would like feedback on the current ones. At the moment, only the `RUN` step has additional arguments shown.

For other steps, would it be useful to see the arguments for other operations like the copy source or destination?

If an argument is using the default value or is empty, would it still be helpful to show?

Are there any additional things that would be useful to be able to see in the inspection window?

## Official Implementations

The following are officially supported plugins for invoking the debug adapter.
Please refer to the documentation in each of these repositories for installation instructions.

- [Visual Studio Code](https://github.com/docker/vscode-extension/)
- [Neovim](https://github.com/docker/nvim-dap-docker/)
