#include <stdio.h>
#include <unistd.h>

int main(int argc, char **argv)
{
	printf("syncing...");
	sync();
	printf("done. rebooting...\n");
	reboot();
	return 0;
}
