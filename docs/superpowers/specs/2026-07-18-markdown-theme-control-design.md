# Markdown Viewer Theme Control Design

## Purpose

Add a visible theme control to the Markdown whiteboard viewer so a reader can explicitly choose System, Light, or Dark without using browser developer tools. The control changes only browser presentation; it does not modify the whiteboard, call a server API, or alter capability URLs.

## Interaction and layout

The viewer displays a compact floating `Theme: <selection>` button in the top-left corner. The page reserves enough top space that the control never covers Markdown content. On narrow screens, the control remains compact and inside the viewport.

Activating the button opens a small menu containing System, Light, and Dark. The current selection has a visible checkmark. Selecting an option applies it immediately, closes the menu, updates the button label, and returns focus to the button.

The menu supports keyboard and assistive-technology use:

- the trigger exposes its expanded state and menu relationship;
- Tab can reach the trigger and each choice;
- Escape closes the menu and restores trigger focus;
- clicking outside the control closes the menu;
- focus styling remains clearly visible in both light and dark modes.

## Architecture and data flow

The control is created by the existing browser viewer module and inserted before the Markdown content. It has no dependency beyond the current DOM APIs.

The control reads the viewer controller's selected theme and delegates changes to its existing `setTheme(value)` method. That method remains the single theme transition path: it normalizes the value, persists it under `agent-whiteboard-theme`, updates the system-theme media-query subscription, changes the document theme, and re-renders Mermaid diagrams with matching colors.

The button label reflects the selected preference, not merely the resolved color. For example, when System resolves to dark, the button still reads `Theme: System`.

If local storage is unavailable, the selected theme still applies for the current page session. Existing storage failures remain non-fatal.

## Styling

The viewer stylesheet defines the control, menu, option, active state, hover state, and focus-visible state using the existing theme variables. Additional variables may be added for elevated surfaces and subtle shadows, but no external assets, fonts, images, or dependencies are introduced.

The menu uses a high stacking level and an opaque themed background. Its placement is stable and does not change the Markdown document width. Reduced-motion users receive no animated transition requirement.

## Error handling

Selecting a theme waits for the existing asynchronous Mermaid re-render. The visual theme and selected label update immediately through the existing theme state. If rendering an individual Mermaid diagram fails, the existing per-diagram error block remains responsible for that failure; the theme menu stays usable.

Opening, closing, or selecting from the menu does not transmit data or create server-side state.

## Testing

Unit tests will verify:

- initial button text matches the stored preference;
- the menu exposes all three choices and marks the current one;
- selecting Light, Dark, and System delegates through the theme controller and persists the choice;
- Escape and outside clicks close the menu;
- destroy removes control event listeners along with the existing media-query subscription.

Browser tests will publish a real Markdown whiteboard, operate the menu using visible controls, verify the document theme and persisted value for each choice, and confirm Mermaid labels remain visible after theme changes. The deterministic asset build, JavaScript unit suite, browser suite, and full Go suite must all pass.

## Acceptance criteria

- A top-left Theme button is visible without covering Markdown content.
- System, Light, and Dark are explicit selectable options.
- The selected option is visible and persists across reloads.
- System follows operating-system color-scheme changes.
- Keyboard dismissal and focus restoration work.
- Mermaid diagrams re-render with visible labels after every theme change.
- No server API, external resource, or new dependency is introduced.
