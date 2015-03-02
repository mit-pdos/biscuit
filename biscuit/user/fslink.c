#include <litc.h>

int main(int argc, char **argv)
{
	if (link("/biscuit", "/spin") >= 0)
		errx(-1, "should have failed");

	if (link("/boot/uefi/readme.txt", "/crap") != 0)
		errx(-1, "should have suceeded");
	return 0;
}
