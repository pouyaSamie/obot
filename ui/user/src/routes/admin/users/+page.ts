import { handleRouteError } from '$lib/errors';
import { AdminService, UserService, type OrgUser } from '$lib/services';
import type { AuthProvider } from '$lib/services/admin/types';
import { profile } from '$lib/stores';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	let users: OrgUser[] = [];
	let authProviders: AuthProvider[] = [];
	try {
		users = await UserService.listUsers({ fetch });
		authProviders = await AdminService.listAuthProviders({ fetch });
	} catch (err) {
		handleRouteError(err, `/users`, profile.current);
	}

	return {
		users,
		authProviders
	};
};
