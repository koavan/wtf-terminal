// Snippets: one-line commands with {{placeholders}}, or runbooks of steps,
// from the snippets files (internal/snippets). Like
// internal/uicore/snippets.go.
import Foundation

public typealias Snippet = Go.snippets.Snippet

extension Snippet {
    public var isRunbook: Bool { !steps.isEmpty }

    /// The values it asks for, from the command or each step, in order of first use.
    public var fields: [Field] {
        Placeholders.fields(in: isRunbook ? steps.map(\.command) : [command],
                            params: params.map { Param(name: $0.name, description: $0.description, default: $0.default, choices: $0.choices, raw: $0.raw) })
    }

    /// The side pane's filter: name, description, command, steps and tags.
    public func matches(_ query: String) -> Bool {
        let q = query.trimmingCharacters(in: .whitespaces).lowercased()
        guard !q.isEmpty else { return true }
        return ([name, description, command] + steps.map(\.command) + tags).joined(separator: " ").lowercased().contains(q)
    }

    /// The row's second line: the description, the step titles, or the command.
    public var subtitle: String {
        if !description.isEmpty { return description }
        if isRunbook { return steps.enumerated().map { $1.title.isEmpty ? "step \($0 + 1)" : $1.title }.joined(separator: " → ") }
        return command
    }

    /// What it runs, for a tooltip: the command, or the steps numbered.
    public var tooltip: String {
        isRunbook ? steps.enumerated().map { "\($0 + 1). \($1.command)" }.joined(separator: "\n") : command
    }
}

extension Field {
    /// The value a field starts with in the run dialog: its default worked
    /// out from the shell, else the first choice.
    public func initialValue(_ facts: ShellFacts) -> String {
        let v = Placeholders.resolve(self.default, facts)
        return v.isEmpty ? (choices.first ?? "") : v
    }
}
