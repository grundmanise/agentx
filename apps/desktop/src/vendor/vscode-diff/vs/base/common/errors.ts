/*---------------------------------------------------------------------------------------------
 *  Copyright (c) Microsoft Corporation. All rights reserved.
 *  Licensed under the MIT License. See License.txt in the project root for license information.
 *--------------------------------------------------------------------------------------------*/

/**
 * This error indicates a bug.
 * Do not throw this for invalid user input.
 * Only catch this error to recover gracefully from bugs.
 */
export class BugIndicatingError extends Error {
	constructor(message?: string) {
		super(message || 'An unexpected bug occurred.');
		Object.setPrototypeOf(this, BugIndicatingError.prototype);

		// Because we know for sure only buggy code throws this,
		// we definitely want to break here and fix the bug.
		// debugger;
	}
}

// agentx: VS Code's default unexpected-error handler (`ErrorHandler` above `onUnexpectedError` in
// upstream errors.ts), without its listeners, cancellation check and telemetry: rethrow the error
// on a later task, so that a failed assertion shows in the console and never stops the caller.
export function onUnexpectedError(e: any): undefined {
	setTimeout(() => {
		throw e;
	}, 0);
	return undefined;
}
