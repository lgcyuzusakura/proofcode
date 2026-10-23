// Trusted local code. Keep logs on stderr and pass signal to async operations.
export const tools = [{
  name: "project_brief",
  description: "Create a compact project brief from a topic and requirements.",
  inputSchema: {
    type: "object",
    properties: { topic: { type: "string", minLength: 1, maxLength: 200 }, requirements: { type: "array", items: { type: "string", maxLength: 300 }, maxItems: 12 } },
    required: ["topic"],
    additionalProperties: false
  },
  async execute({ topic, requirements = [] }, { signal }) {
    signal.throwIfAborted();
    return { topic, requirements, acceptance: requirements.map(value => ({ requirement: value, verified: false })) };
  }
}];
