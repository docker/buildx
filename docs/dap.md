# Debug Adapter Protocol

## What is the debug adapter protocol?

The debug adapter protocol [DAP](https://microsoft.github.io/debug-adapter-protocol/overview) is a protocol created by Microsoft to standardize an abstract protocol for how a development tool (such as VS Code) communicates with concrete debuggers.

Many [popular editors](https://microsoft.github.io/debug-adapter-protocol/implementors/tools/) now support DAP as the primary method of launching debuggers. This includes **VS Code** and **Neovim**. There are also other editors, such as **Jetbrains IDEs** where DAP is not the primary method of launching debuggers, but is available with plugins.

## Features

- Pause on exception.
- Set breakpoints on instructions.
- Step next and continue.
- Open terminal in an intermediate container image.
- File explorer.

## Limitations

- The debugger cannot differentiate between identical `FROM` directives.
- Invalid `args` in launch request may not produce an error in the UI.
- Does not support arbitrary pausing.
- Output is always the plain text printer.
- File explorer does not work when pausing on an exception.

## Future Improvements

- Support for Bake.
- Backwards stepping.
- Better UI for errors with invalid arguments.

## We would like feedback on

- Step/pause locations.
- Variable inspections.
- Additional information that would be helpful while debugging.
- Annoyances or difficulties with the current implementation.

### Step/pause Locations

Execution is paused **before** the step has been executed. Due to the way Dockerfiles are written, this sometimes creates
some unclear visuals regarding where the pause happened.

For the last command in a stage, step **next** will highlight the same instruction twice. One of these is before the execution and the second is after. For every other command, they are only highlighted before the command is executed. It is not currently possible to set a breakpoint at the end of a stage. You must set the breakpoint on the last step and then use step **next**.

When a command has multiple parents, step **into** will step into one of the parents. Step **out** will then return from that stage. This will continue until there are no additional parents. There is currently no way to tell the difference between which parents have executed and which ones have not.

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

### Plugin Integration Guidelines

An official debug adapter plugin must meet the requirements indicated in this section.

The plugin MUST support `args` as a launch argument. The `args` value must be an array and it MUST be passed at the end of the tool invocation.

The plugin MUST support `builder` as a launch argument. If present, `builder` will be passed as `--builder <value>` after the invocation of `buildx` but before the `build` argument.

The plugin MUST provide a way to run the DAP command through the `docker` command (i.e. `docker buildx`).

The plugin SHOULD provide a way to run the `buildx` binary in standalone mode.

The plugin SHOULD invoke the DAP command from the workspace root. If it cannot invoke the command from the workspace root for some reason, it MUST invoke it from the current working directory.
