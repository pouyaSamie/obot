import { handleRouteError } from '$lib/errors';
import { NanobotService } from '$lib/services';
import type { Skill } from '$lib/services/nanobot/types';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch, parent }) => {
	const { profile } = await parent();
	let skills: Skill[] = [];

	try {
		skills = await NanobotService.listSkills({ fetch, dontLogErrors: true });
	} catch (err) {
		handleRouteError(err, `/skills`, profile);
	}

	return {
		skills
	};
};
