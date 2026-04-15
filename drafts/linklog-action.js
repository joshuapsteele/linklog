// LinkLog — Drafts Action
//
// Draft format:
//   Line 1: URL
//   Line 2: comma-separated tags (optional)
//   Line 3+: commentary (optional)
//
// Example:
//   https://example.com/article
//   go,webdev
//   This is a sharp take on simplicity in web development.

// Retrieve or prompt for the API token on first use.
var credential = Credential.create("LinkLog", "Enter your LinkLog API token");
credential.addTextField("token", "API Token");
credential.authorize();

var token = credential.getValue("token");

var lines = draft.content.split("\n");
var url = lines[0].trim();
var tags = lines.length > 1 ? lines[1].trim() : "";
var commentary = lines.length > 2 ? lines.slice(2).join("\n").trim() : "";

if (!url.startsWith("http://") && !url.startsWith("https://")) {
    app.displayErrorMessage("First line must be a URL");
    context.fail();
} else {
    var http = HTTP.create();
    var response = http.request({
        url: "https://links.joshuapsteele.com/api/links",
        method: "POST",
        headers: {
            "Content-Type": "application/json",
            "Authorization": "Bearer " + token
        },
        data: {
            url: url,
            commentary: commentary,
            tags: tags
        }
    });

    var payload = {};
    try {
        payload = JSON.parse(response.responseText || "{}");
    } catch (e) {
        payload = {};
    }

    if (response.success) {
        app.displaySuccessMessage(payload.message || "Link posted!");
        // Archive the draft after successful posting.
        draft.isArchived = true;
        draft.update();
    } else {
        app.displayErrorMessage((payload.error || "Failed") + " (" + response.statusCode + ")");
        context.fail();
    }
}
