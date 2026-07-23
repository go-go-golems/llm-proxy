const A = require("tinyidp").v1;

// Phase 1 has an operator-created local account only. A later reviewed
// invitation/signup policy replaces this deny-by-default program.
module.exports = A.program("signup-disabled", program => {
  const start = A.lambda("signup.start", {
    input: "signupStartInput", output: "signupResult",
    outcomes: ["deny"], effects: [], capabilities: [],
    timeoutMs: 250, maxCapabilityCalls: 0, maxOutputBytes: 1024,
    run: () => A.result.deny("signup_disabled"),
  });
  program.workflow("signup", {
    version: 1, entry: "start", handlers: {start}, edges: [],
  });
});
